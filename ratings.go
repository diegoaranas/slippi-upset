package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	api   = "https://internal.slippi.gg/graphql"
	query = `query U($cc: String) { getUser(connectCode: $cc) {
  rankedNetplayProfile { ratingOrdinal ratingUpdateCount }
  rankedNetplayProfileHistory { ratingOrdinal ratingUpdateCount season { name } } } }`
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Ratings are nil when unknown. Current is nil if unranked this season. Peak is the
// best season including the current one; PastPeak leaves the current season out.
type Ratings struct {
	Current, Peak, PastPeak *float64
}

type profile struct {
	RatingOrdinal     float64 `json:"ratingOrdinal"`
	RatingUpdateCount int     `json:"ratingUpdateCount"`
}

func fetchRatings(code string) (Ratings, error) {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]string{"cc": code}})
	req, err := http.NewRequest("POST", api, bytes.NewReader(body))
	if err != nil {
		return Ratings{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "slippi-upset-alert")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Ratings{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Ratings{}, fmt.Errorf("slippi.gg returned %s", resp.Status)
	}
	var out struct {
		Data struct {
			GetUser *struct {
				Current *profile  `json:"rankedNetplayProfile"`
				History []profile `json:"rankedNetplayProfileHistory"`
			} `json:"getUser"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Ratings{}, err
	}
	user := out.Data.GetUser
	if user == nil {
		return Ratings{}, nil
	}
	var r Ratings
	if c := user.Current; c != nil && c.RatingUpdateCount > 0 {
		r.Current = &c.RatingOrdinal
	}
	for i := range user.History {
		if s := &user.History[i]; s.RatingUpdateCount > 0 && (r.PastPeak == nil || s.RatingOrdinal > *r.PastPeak) {
			r.PastPeak = &s.RatingOrdinal
		}
	}
	r.Peak = r.PastPeak
	if higher(r.Current, r.Peak) || r.Peak == nil {
		r.Peak = r.Current
	}
	return r, nil
}

// higher reports whether a is known and beats b.
func higher(a, b *float64) bool {
	return a != nil && b != nil && *a > *b
}

// isHigher uses the same rule as the win sounds: their current rating beats
// yours, or their best season beats yours.
func isHigher(mine, theirs Ratings) bool {
	return higher(theirs.Current, mine.Current) || higher(theirs.Peak, mine.Peak)
}

func fmtRating(x *float64) string {
	if x == nil {
		return "unranked"
	}
	return fmt.Sprintf("%.1f", *x)
}
