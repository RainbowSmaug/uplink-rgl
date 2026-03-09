package main

import (
	"encoding/json"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ensureCoverCacheDir returns (and creates if needed) ~/.cache/uplink-rgl/covers.
func ensureCoverCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "uplink-rgl", "covers")
	return dir, os.MkdirAll(dir, 0755)
}

// cachedApolloImage fetches a cover by filename from uplink-host's covers
// file server (port 47991). Files are already PNG — no transcoding needed.
func cachedApolloImage(imageName, baseURL, cacheDir string) string {
	localPath := filepath.Join(cacheDir, imageName)

	if info, err := os.Stat(localPath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			return localPath
		}
	}

	resp, err := http.Get(baseURL + imageName)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		if _, err := os.Stat(localPath); err == nil {
			return localPath // stale cache beats nothing
		}
		return ""
	}
	defer resp.Body.Close()

	tmp := localPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return ""
	}
	f.Close()
	if err := os.Rename(tmp, localPath); err != nil {
		os.Remove(tmp)
		return ""
	}
	return localPath
}

// steamCoverURLs returns CDN URLs to try in order for a given Steam App ID,
// ending with a Steam Store API lookup to get the current hashed image URL for
// games whose assets have been moved off the legacy CDN paths.
func steamCoverURLs(steamID string) []string {
	base := "https://steamcdn-a.akamaihd.net/steam/apps/" + steamID + "/"
	urls := []string{
		base + "library_600x900.jpg",
		base + "library_600x900_2x.jpg",
		base + "header.jpg",
	}
	// Newer games (or games whose assets were reorganised) only have
	// content-addressed URLs under shared.akamai.steamstatic.com. We can
	// discover the current URL via the store API.
	if u := steamStoreHeaderURL(steamID); u != "" {
		urls = append(urls, u)
	}
	return urls
}

// steamStoreHeaderURL fetches the header_image URL from the Steam Store API.
// Returns "" on any error so callers can treat it as a missing URL.
func steamStoreHeaderURL(steamID string) string {
	url := "https://store.steampowered.com/api/appdetails?appids=" + steamID + "&filters=basic,header_image"
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var result map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			HeaderImage string `json:"header_image"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if entry, ok := result[steamID]; ok && entry.Success {
		return entry.Data.HeaderImage
	}
	return ""
}

// cachedCover returns the local path to a game's cover PNG, downloading/refreshing
// from Steam CDN if the cached file is missing or older than 24 hours.
func cachedCover(steamID, cacheDir string) string {
	localPath := filepath.Join(cacheDir, "steam_"+steamID+".png")

	if info, err := os.Stat(localPath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			return localPath
		}
	}

	for _, url := range steamCoverURLs(steamID) {
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		img, err := jpeg.Decode(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// Write to a temp file then rename so we never leave a partial PNG.
		tmp := localPath + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return ""
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			os.Remove(tmp)
			return ""
		}
		f.Close()
		if err := os.Rename(tmp, localPath); err != nil {
			os.Remove(tmp)
			return ""
		}
		return localPath
	}

	// Return stale cache if all downloads fail rather than nothing
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}
	return ""
}
