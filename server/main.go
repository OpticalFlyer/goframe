package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PhotoMetadata struct {
	Hash      string    `json:"hash"`
	Filename  string    `json:"filename"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PhotoStorage struct {
	baseDir string
	mu      sync.RWMutex
	photos  map[string]PhotoMetadata
}

func NewPhotoStorage(baseDir string) (*PhotoStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	ps := &PhotoStorage{
		baseDir: baseDir,
		photos:  make(map[string]PhotoMetadata),
	}

	return ps, ps.loadExistingPhotos()
}

func (ps *PhotoStorage) loadExistingPhotos() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	return filepath.Walk(ps.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".jpeg" {
			hash, err := ps.hashFile(path)
			if err != nil {
				return err
			}
			ps.photos[hash] = PhotoMetadata{
				Hash:      hash,
				Filename:  info.Name(),
				UpdatedAt: info.ModTime(),
			}
		}
		return nil
	})
}

func (ps *PhotoStorage) hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (ps *PhotoStorage) List() []PhotoMetadata {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	list := make([]PhotoMetadata, 0, len(ps.photos))
	for _, photo := range ps.photos {
		list = append(list, photo)
	}
	return list
}

func (ps *PhotoStorage) Get(hash string) (string, error) {
	ps.mu.RLock()
	photo, exists := ps.photos[hash]
	ps.mu.RUnlock()

	if !exists {
		return "", os.ErrNotExist
	}

	return filepath.Join(ps.baseDir, photo.Filename), nil
}

func (ps *PhotoStorage) Add(filename string, reader io.Reader) (*PhotoMetadata, error) {
	tempPath := filepath.Join(ps.baseDir, "tmp-"+filename)
	f, err := os.Create(tempPath)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), reader); err != nil {
		f.Close()
		os.Remove(tempPath)
		return nil, err
	}
	f.Close()

	hash := hex.EncodeToString(h.Sum(nil))
	finalPath := filepath.Join(ps.baseDir, filename)

	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return nil, err
	}

	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, err
	}

	meta := PhotoMetadata{
		Hash:      hash,
		Filename:  filename,
		UpdatedAt: info.ModTime(),
	}

	ps.mu.Lock()
	ps.photos[hash] = meta
	ps.mu.Unlock()

	return &meta, nil
}

func (ps *PhotoStorage) Delete(hash string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	photo, exists := ps.photos[hash]
	if !exists {
		return os.ErrNotExist
	}

	path := filepath.Join(ps.baseDir, photo.Filename)
	if err := os.Remove(path); err != nil {
		return err
	}

	delete(ps.photos, hash)
	return nil
}

type Server struct {
	photos *PhotoStorage
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	photos := s.photos.List()
	json.NewEncoder(w).Encode(photos)
}

func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	hash := filepath.Base(r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		path, err := s.photos.Get(hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, path)

	case http.MethodPost:
		file, header, err := r.FormFile("photo")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		meta, err := s.photos.Add(header.Filename, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(meta)

	case http.MethodDelete:
		if err := s.photos.Delete(hash); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func main() {
	var (
		port     = flag.String("port", "8080", "Server port")
		photoDir = flag.String("photos", "", "Photo storage directory")
	)
	flag.Parse()

	if *photoDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		*photoDir = filepath.Join(homeDir, ".goframe", "photos")
	}

	photos, err := NewPhotoStorage(*photoDir)
	if err != nil {
		log.Fatal(err)
	}

	server := &Server{photos: photos}

	mux := http.NewServeMux()
	mux.HandleFunc("/photos/list", server.handleList)
	mux.HandleFunc("/photos/", server.handlePhoto)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("Starting server on %s", addr)
	log.Printf("Photo directory: %s", *photoDir)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
