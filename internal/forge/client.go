package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Persona struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Capabilities []string `json:"capabilities"`
}

// ModelTierStats holds effectiveness metrics for a single model tier,
// as returned by PromptForge's /api/v1/effectiveness/model-tiers endpoint.
type ModelTierStats struct {
	CorrectionRate   float64 `json:"correction_rate"`
	AvgEffectiveness float64 `json:"avg_effectiveness"`
	SessionCount     int     `json:"session_count"`
}

type Client interface {
	ListPersonas(ctx context.Context) ([]Persona, error)
	GetAgentsByCapability(ctx context.Context, scope string) ([]Persona, error)
	GetModelEffectiveness(ctx context.Context) (map[string]ModelTierStats, error)
}

type HTTPClient struct {
	baseURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	cache     []Persona
	cacheTime time.Time
	cacheTTL  time.Duration

	effectivenessMu        sync.RWMutex
	effectivenessCache     map[string]ModelTierStats
	effectivenessCacheTime time.Time
	effectivenessCacheTTL  time.Duration
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:               baseURL,
		httpClient:            &http.Client{Timeout: 10 * time.Second},
		cacheTTL:              60 * time.Second,
		effectivenessCacheTTL: 5 * time.Minute,
	}
}

type promptListItem struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type versionResponse struct {
	Version int `json:"version"`
	Content struct {
		Sections []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"sections"`
	} `json:"content"`
}

func (c *HTTPClient) ListPersonas(ctx context.Context) ([]Persona, error) {
	c.mu.RLock()
	if c.cache != nil && time.Since(c.cacheTime) < c.cacheTTL {
		cached := make([]Persona, len(c.cache))
		copy(cached, c.cache)
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	personas, err := c.fetchPersonas(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache = personas
	c.cacheTime = time.Now()
	c.mu.Unlock()

	return personas, nil
}

func (c *HTTPClient) fetchPersonas(ctx context.Context) ([]Persona, error) {
	// Get all prompts
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/prompts", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("promptforge: %d %s", resp.StatusCode, string(body))
	}

	var items []promptListItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}

	// For each persona, fetch latest version to get capabilities
	var personas []Persona
	for _, item := range items {
		if item.Type != "persona" {
			continue
		}
		p := Persona{ID: item.ID, Slug: item.Slug, Name: item.Name, Type: item.Type}

		// Fetch latest version (try version 2 first since we added capabilities as v2, fallback to 1)
		caps := c.fetchCapabilities(ctx, item.Slug)
		p.Capabilities = caps
		personas = append(personas, p)
	}
	return personas, nil
}

func (c *HTTPClient) fetchCapabilities(ctx context.Context, slug string) []string {
	// Try versions in descending order (most recent first)
	for v := 10; v >= 1; v-- {
		url := fmt.Sprintf("%s/api/v1/prompts/%s/versions/%d", c.baseURL, slug, v)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			continue
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}

		var ver versionResponse
		if err := json.Unmarshal(body, &ver); err != nil {
			continue
		}

		for _, s := range ver.Content.Sections {
			if s.ID == "capabilities" {
				return ParseCapabilities(s.Content)
			}
		}
	}
	return nil
}

func (c *HTTPClient) GetAgentsByCapability(ctx context.Context, scope string) ([]Persona, error) {
	all, err := c.ListPersonas(ctx)
	if err != nil {
		return nil, err
	}
	var matched []Persona
	for _, p := range all {
		for _, cap := range p.Capabilities {
			if strings.EqualFold(cap, scope) {
				matched = append(matched, p)
				break
			}
		}
	}
	return matched, nil
}

// GetModelEffectiveness fetches model tier effectiveness stats from PromptForge.
// Results are cached with a 5-minute TTL since effectiveness data changes slowly.
func (c *HTTPClient) GetModelEffectiveness(ctx context.Context) (map[string]ModelTierStats, error) {
	c.effectivenessMu.RLock()
	if c.effectivenessCache != nil && time.Since(c.effectivenessCacheTime) < c.effectivenessCacheTTL {
		result := make(map[string]ModelTierStats, len(c.effectivenessCache))
		for k, v := range c.effectivenessCache {
			result[k] = v
		}
		c.effectivenessMu.RUnlock()
		return result, nil
	}
	c.effectivenessMu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/effectiveness/model-tiers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("promptforge: %d %s", resp.StatusCode, string(body))
	}

	var stats map[string]ModelTierStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, err
	}

	c.effectivenessMu.Lock()
	c.effectivenessCache = stats
	c.effectivenessCacheTime = time.Now()
	c.effectivenessMu.Unlock()

	return stats, nil
}

func ParseCapabilities(content string) []string {
	parts := strings.Split(content, ",")
	var caps []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			caps = append(caps, strings.ToLower(p))
		}
	}
	return caps
}
