package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	v1Depth3URL    = "https://provinces.open-api.vn/api/?depth=3"
	v2ProvincesURL = "https://provinces.open-api.vn/api/v2/p/?depth=3"
	v2WardsURL     = "https://provinces.open-api.vn/api/v2/w/"
)

type v1Province struct {
	Code      int          `json:"code"`
	Name      string       `json:"name"`
	Districts []v1District `json:"districts"`
}

type v1District struct {
	Code  int      `json:"code"`
	Name  string   `json:"name"`
	Wards []v1Ward `json:"wards"`
}

type v1Ward struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// fetchWithRetry fetches an HTTP endpoint with timeout and exponential backoff retries.
func fetchWithRetry(ctx context.Context, client *http.Client, url string, maxAttempts int) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "hsk-vinh-geospatial-snapshot/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if !json.Valid(body) {
					lastErr = fmt.Errorf("invalid JSON response from %s", url)
				} else {
					return body, nil
				}
			} else {
				lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			}
		}

		if attempt < maxAttempts {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, fmt.Errorf("failed after %d attempts for %s: %w", maxAttempts, url, lastErr)
}

func main() {
	outDir := flag.String("out", "internal/location/data", "output directory for geospatial JSON snapshots")
	timeout := flag.Duration("timeout", 20*time.Second, "per-request HTTP timeout")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout}
	ctx := context.Background()

	targets := []struct {
		url      string
		filename string
	}{
		{v1Depth3URL, "v1_snapshot.json"},
		{v2ProvincesURL, "v2_provinces_snapshot.json"},
		{v2WardsURL, "v2_wards_snapshot.json"},
	}

	for _, t := range targets {
		fmt.Printf("Fetching %s...\n", t.url)
		body, err := fetchWithRetry(ctx, client, t.url, 3)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error fetching %s: %v\n", t.url, err)
			if errors.Is(err, context.DeadlineExceeded) {
				os.Exit(2)
			}
			os.Exit(1)
		}

		dest := filepath.Join(*outDir, t.filename)
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", dest, err)
			os.Exit(1)
		}
		fmt.Printf("Saved %s (%d bytes)\n", dest, len(body))

		if t.filename == "v1_snapshot.json" {
			var provs []v1Province
			if err := json.Unmarshal(body, &provs); err == nil {
				dCount, wCount := 0, 0
				for _, p := range provs {
					dCount += len(p.Districts)
					for _, d := range p.Districts {
						wCount += len(d.Wards)
					}
				}
				fmt.Printf("Verified v1 summary: %d provinces, %d districts, %d wards\n", len(provs), dCount, wCount)
			}
		}
	}
}

