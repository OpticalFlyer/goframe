// photo_sync.go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

type PhotoSync struct {
	serverURL      string
	photoDir       string
	localHashes    map[string]bool
	client         *http.Client
	lastError      error
	retryBackoff   time.Duration
	syncMutex      sync.Mutex
	isSyncing      bool
	onSyncComplete func()
}

func NewPhotoSync(serverURL, photoDir string, onSyncComplete func()) *PhotoSync {
	return &PhotoSync{
		serverURL:      serverURL,
		photoDir:       photoDir,
		localHashes:    make(map[string]bool),
		client:         &http.Client{Timeout: 30 * time.Second},
		retryBackoff:   1 * time.Minute,
		onSyncComplete: onSyncComplete,
	}
}

func (ps *PhotoSync) hashFile(path string) (string, error) {
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

func (ps *PhotoSync) loadLocalHashes() error {
	files, err := os.ReadDir(ps.photoDir)
	if err != nil {
		return err
	}

	ps.localHashes = make(map[string]bool)
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".jpeg" {
			hash, err := ps.hashFile(filepath.Join(ps.photoDir, file.Name()))
			if err != nil {
				continue
			}
			ps.localHashes[hash] = true
		}
	}
	return nil
}

func (ps *PhotoSync) Sync() error {
	startTime := time.Now()
	fmt.Println("Starting photo sync...")

	if !ps.syncMutex.TryLock() {
		fmt.Println("Sync already in progress, skipping")
		return fmt.Errorf("sync already in progress")
	}
	defer ps.syncMutex.Unlock()

	fmt.Printf("Fetching photo list from %s...\n", ps.serverURL)
	resp, err := ps.client.Get(ps.serverURL + "/photos/list")
	if err != nil {
		ps.lastError = fmt.Errorf("server connection failed: %v", err)
		ps.retryBackoff *= 2
		if ps.retryBackoff > 1*time.Hour {
			ps.retryBackoff = 1 * time.Hour
		}
		fmt.Printf("Connection failed, will retry in %v\n", ps.retryBackoff)
		return ps.lastError
	}

	ps.retryBackoff = 1 * time.Minute
	ps.lastError = nil
	defer resp.Body.Close()

	var remotePhotos []PhotoMetadata
	if err := json.NewDecoder(resp.Body).Decode(&remotePhotos); err != nil {
		fmt.Printf("Failed to decode remote photo list: %v\n", err)
		return err
	}
	fmt.Printf("Found %d photos on server\n", len(remotePhotos))

	fmt.Println("Loading local photo hashes...")
	if err := ps.loadLocalHashes(); err != nil {
		fmt.Printf("Failed to load local hashes: %v\n", err)
		return err
	}
	fmt.Printf("Found %d photos locally\n", len(ps.localHashes))

	// Delete local photos not in remote list
	deleteCount := 0
	fmt.Println("Checking for photos to delete...")
	for hash := range ps.localHashes {
		found := false
		for _, remote := range remotePhotos {
			if remote.Hash == hash {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Deleting photo %s...\n", hash[:8])
			if err := ps.deleteLocalPhoto(hash); err != nil {
				fmt.Printf("Error deleting photo %s: %v\n", hash[:8], err)
			} else {
				deleteCount++
			}
		}
	}

	// Download missing photos
	downloadCount := 0
	fmt.Println("Checking for new photos to download...")
	for _, remote := range remotePhotos {
		if !ps.localHashes[remote.Hash] {
			fmt.Printf("Downloading %s (%s)...\n", remote.Filename, remote.Hash[:8])
			if err := ps.downloadPhoto(remote); err != nil {
				fmt.Printf("Error downloading photo %s: %v\n", remote.Hash[:8], err)
			} else {
				downloadCount++
			}
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\nSync completed in %.1f seconds\n", elapsed.Seconds())
	fmt.Printf("Photos deleted: %d\n", deleteCount)
	fmt.Printf("Photos downloaded: %d\n", downloadCount)

	if downloadCount > 0 || deleteCount > 0 {
		if ps.onSyncComplete != nil {
			ps.onSyncComplete()
		}
	}

	return nil
}

func (ps *PhotoSync) deleteLocalPhoto(hash string) error {
	files, err := os.ReadDir(ps.photoDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		path := filepath.Join(ps.photoDir, file.Name())
		fileHash, err := ps.hashFile(path)
		if err != nil {
			continue
		}
		if fileHash == hash {
			return os.Remove(path)
		}
	}
	return nil
}

func (ps *PhotoSync) downloadPhoto(photo PhotoMetadata) error {
	resp, err := ps.client.Get(fmt.Sprintf("%s/photos/%s", ps.serverURL, photo.Hash))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	path := filepath.Join(ps.photoDir, photo.Filename)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
