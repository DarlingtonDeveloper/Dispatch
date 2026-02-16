// seed_backlog.go — standalone script to parse TODO.md and seed backlog items via Dispatch API.
//
// Usage:
//
//	go run scripts/seed_backlog.go -todo /path/to/TODO.md -api http://localhost:8600 -agent system
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

type backlogItem struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	ItemType       string   `json:"item_type"`
	Status         string   `json:"status,omitempty"`
	Domain         string   `json:"domain,omitempty"`
	ManualPriority *float64 `json:"manual_priority,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Source         string   `json:"source"`
}

// Priority emoji to urgency mapping
var priorityMap = map[string]float64{
	"🔴": 0.95, // P0
	"🟠": 0.75, // P1
	"🟡": 0.50, // P2
	"🟢": 0.25, // P3
}

// Sections to skip
var skipSections = map[string]bool{
	"personal":       true,
	"career":         true,
	"health":         true,
	"growth":         true,
	"personal/career": true,
}

func main() {
	todoPath := flag.String("todo", "TODO.md", "path to TODO.md file")
	apiURL := flag.String("api", "http://localhost:8600", "Dispatch API base URL")
	agentID := flag.String("agent", "system", "X-Agent-ID header value")
	dryRun := flag.Bool("dry-run", false, "print items without posting")
	flag.Parse()

	f, err := os.Open(*todoPath)
	if err != nil {
		log.Fatalf("open TODO.md: %v", err)
	}
	defer f.Close()

	var items []backlogItem
	var currentSection string
	var skipCurrent bool
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()

		// Detect section headers
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			header := strings.TrimLeft(line, "# ")
			header = strings.TrimSpace(header)
			currentSection = strings.ToLower(header)

			skipCurrent = false
			for skip := range skipSections {
				if strings.Contains(currentSection, skip) {
					skipCurrent = true
					break
				}
			}
			continue
		}

		if skipCurrent {
			continue
		}

		// Parse TODO items: - [ ] or - [x]
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [") {
			continue
		}

		isDone := strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]")
		text := trimmed
		if isDone {
			text = strings.TrimPrefix(text, "- [x] ")
			text = strings.TrimPrefix(text, "- [X] ")
		} else {
			text = strings.TrimPrefix(text, "- [ ] ")
		}

		// Detect priority emoji
		var urgency *float64
		for emoji, u := range priorityMap {
			if strings.Contains(text, emoji) {
				val := u
				urgency = &val
				text = strings.ReplaceAll(text, emoji, "")
				text = strings.TrimSpace(text)
				break
			}
		}

		// Derive domain from section
		domain := deriveDomain(currentSection)

		item := backlogItem{
			Title:          text,
			ItemType:       "task",
			Domain:         domain,
			ManualPriority: urgency,
			Source:         "seed",
			Labels:         []string{"from-todo"},
		}

		if isDone {
			item.Status = "done"
		}

		items = append(items, item)
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("scan TODO.md: %v", err)
	}

	log.Printf("parsed %d items from %s", len(items), *todoPath)

	if *dryRun {
		for i, item := range items {
			status := "backlog"
			if item.Status != "" {
				status = item.Status
			}
			urgencyStr := "none"
			if item.ManualPriority != nil {
				urgencyStr = fmt.Sprintf("%.2f", *item.ManualPriority)
			}
			fmt.Printf("[%d] %s (domain=%s, urgency=%s, status=%s)\n", i+1, item.Title, item.Domain, urgencyStr, status)
		}
		return
	}

	client := &http.Client{}
	created, skipped := 0, 0
	for _, item := range items {
		body, _ := json.Marshal(item)
		req, err := http.NewRequest("POST", *apiURL+"/api/v1/backlog", bytes.NewReader(body))
		if err != nil {
			log.Printf("skip %q: %v", item.Title, err)
			skipped++
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-ID", *agentID)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("skip %q: %v", item.Title, err)
			skipped++
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusCreated {
			created++
		} else {
			log.Printf("skip %q: status %d", item.Title, resp.StatusCode)
			skipped++
		}
	}

	log.Printf("done: %d created, %d skipped", created, skipped)
}

func deriveDomain(section string) string {
	section = strings.ToLower(section)
	switch {
	case strings.Contains(section, "infra"):
		return "infrastructure"
	case strings.Contains(section, "product"):
		return "product"
	case strings.Contains(section, "ops") || strings.Contains(section, "operation"):
		return "operations"
	case strings.Contains(section, "research"):
		return "research"
	case strings.Contains(section, "agent"):
		return "agents"
	case strings.Contains(section, "api"):
		return "api"
	case strings.Contains(section, "security"):
		return "security"
	default:
		return section
	}
}
