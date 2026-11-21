package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var listenBrainzBaseURL = "https://api.listenbrainz.org"

const (
	lbBatchSize             = 500
	lbMaxRetries            = 3
	submissionClientVersion = "0.1.0"
)

type LBClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type lbSubmit struct {
	ListenType string     `json:"listen_type"`
	Payload    []lbListen `json:"payload"`
}

type lbListen struct {
	ListenedAt    int64           `json:"listened_at"`
	TrackMetadata lbTrackMetadata `json:"track_metadata"`
}

type lbTrackMetadata struct {
	ArtistName     string           `json:"artist_name"`
	TrackName      string           `json:"track_name"`
	ReleaseName    string           `json:"release_name,omitempty"`
	AdditionalInfo lbAdditionalInfo `json:"additional_info"`
}

type lbAdditionalInfo struct {
	TrackNumber             string `json:"tracknumber,omitempty"`
	DurationMs              int    `json:"duration_ms"`
	RecordingMBID           string `json:"recording_mbid,omitempty"`
	MediaPlayer             string `json:"media_player"`
	SubmissionClient        string `json:"submission_client"`
	SubmissionClientVersion string `json:"submission_client_version"`
}

func (c *LBClient) Validate(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/1/validate-token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("validate-token: status %d: %s", resp.StatusCode, body)
	}
	var v struct {
		Valid    bool   `json:"valid"`
		UserName string `json:"user_name"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	if !v.Valid {
		return "", errors.New("ListenBrainz token is not valid")
	}
	return v.UserName, nil
}

func (c *LBClient) Submit(ctx context.Context, listens []Listen) error {
	for start := 0; start < len(listens); start += lbBatchSize {
		end := min(start+lbBatchSize, len(listens))
		if err := c.submitBatch(ctx, listens[start:end]); err != nil {
			return fmt.Errorf("listens %d-%d: %w", start+1, end, err)
		}
	}
	return nil
}

func (c *LBClient) submitBatch(ctx context.Context, batch []Listen) error {
	payload := make([]lbListen, len(batch))
	for i, l := range batch {
		payload[i] = lbListen{
			ListenedAt: l.ListenedAt,
			TrackMetadata: lbTrackMetadata{
				ArtistName:  l.Artist,
				TrackName:   l.Title,
				ReleaseName: l.Album,
				AdditionalInfo: lbAdditionalInfo{
					TrackNumber:             l.TrackNumber,
					DurationMs:              l.DurationSec * 1000,
					RecordingMBID:           l.MBID,
					MediaPlayer:             "Rockbox",
					SubmissionClient:        "tide",
					SubmissionClientVersion: submissionClientVersion,
				},
			},
		}
	}
	body, err := json.Marshal(lbSubmit{ListenType: "import", Payload: payload})
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/1/submit-listens", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Token "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < lbMaxRetries {
			wait := 5
			if v, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Reset-In")); err == nil {
				wait = v
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		return fmt.Errorf("submit-listens: status %d: %s", resp.StatusCode, respBody)
	}
}

func loadToken() (string, error) {
	if t := os.Getenv("LISTENBRAINZ_TOKEN"); t != "" {
		return t, nil
	}
	if path := os.Getenv("LISTENBRAINZ_TOKEN_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	return "", errors.New("no ListenBrainz token: set LISTENBRAINZ_TOKEN or LISTENBRAINZ_TOKEN_FILE")
}
