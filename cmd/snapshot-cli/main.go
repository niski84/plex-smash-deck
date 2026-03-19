// snapshot-cli is a command-line client for the plex-dashboard Movie Snapshots API.
//
// Usage:
//
//	snapshot-cli [--url http://host:port] <command>
//
// Commands:
//
//	list            List all snapshots (newest first)
//	take            Take a new snapshot of your Plex library
//	latest          Show new/missing movies since the last snapshot (the "latest drop")
//	missing         Show all movies that have gone missing across every snapshot pair
//	diff <from> <to> Compare two specific snapshots by ID
//	show <id>        Print every movie in a specific snapshot
//
// Environment:
//
//	PLEX_DASH_URL   Base URL of the running plex-dashboard server (default: http://127.0.0.1:8081)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func baseURL() string {
	if u := os.Getenv("PLEX_DASH_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://127.0.0.1:8081"
}

func apiGet(path string) (map[string]any, error) {
	url := baseURL() + path
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode response: %w\nraw: %s", err, string(body))
	}
	if !envelope.Success {
		return nil, fmt.Errorf("API error: %s", envelope.Error)
	}
	return envelope.Data, nil
}

func apiPost(path string) (map[string]any, error) {
	url := baseURL() + path
	resp, err := http.Post(url, "application/json", nil) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode response: %w\nraw: %s", err, string(body))
	}
	if !envelope.Success {
		return nil, fmt.Errorf("API error: %s", envelope.Error)
	}
	return envelope.Data, nil
}

func fmtTime(raw any) string {
	if raw == nil {
		return "—"
	}
	s, ok := raw.(string)
	if !ok {
		return fmt.Sprintf("%v", raw)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02  15:04:05")
}

func intVal(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func strVal(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func mapVal(v any) map[string]any {
	if v == nil {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func sliceVal(v any) []any {
	if v == nil {
		return nil
	}
	s, _ := v.([]any)
	return s
}

func movieLine(m map[string]any) string {
	title := strVal(m["title"])
	year := intVal(m["year"])
	if year > 0 {
		return fmt.Sprintf("  • %s (%d)", title, year)
	}
	return fmt.Sprintf("  • %s", title)
}

func hr(char string, n int) string { return strings.Repeat(char, n) }

// ── commands ─────────────────────────────────────────────────────────────────

func cmdList() {
	data, err := apiGet("/api/snapshots")
	if err != nil {
		fatal(err)
	}
	snaps := sliceVal(data["snapshots"])
	if len(snaps) == 0 {
		fmt.Println("No snapshots found. Run: snapshot-cli take")
		return
	}
	fmt.Printf("\n%-4s  %-22s  %6s\n", "#", "Captured At", "Movies")
	fmt.Println(hr("─", 38))
	for i, raw := range snaps {
		m := mapVal(raw)
		fmt.Printf("%-4d  %-22s  %6d\n",
			i+1,
			fmtTime(m["capturedAt"]),
			intVal(m["count"]),
		)
	}
	fmt.Printf("\n%d snapshot(s) total.\n\n", len(snaps))
}

func cmdTake() {
	fmt.Println("Fetching movie list from Plex…")
	data, err := apiPost("/api/snapshots")
	if err != nil {
		fatal(err)
	}
	snap := mapVal(data["snapshot"])
	if snap == nil {
		fmt.Println("Snapshot taken (no detail returned).")
		return
	}
	fmt.Printf("\n✓ Snapshot captured!\n")
	fmt.Printf("  ID      : %s\n", strVal(snap["id"]))
	fmt.Printf("  At      : %s\n", fmtTime(snap["capturedAt"]))
	fmt.Printf("  Movies  : %d\n\n", intVal(snap["count"]))
}

func cmdLatest() {
	data, err := apiGet("/api/snapshots/latest-diff")
	if err != nil {
		fatal(err)
	}
	raw := data["diff"]
	if raw == nil {
		fmt.Println("\nNeed at least 2 snapshots to show a diff. Run: snapshot-cli take")
		return
	}
	diff := mapVal(raw)
	from := mapVal(diff["from"])
	to := mapVal(diff["to"])
	added := sliceVal(diff["added"])
	removed := sliceVal(diff["removed"])
	net := intVal(diff["netChange"])

	fmt.Println()
	fmt.Println(hr("═", 60))
	fmt.Println("  LATEST DROP  —  new movies since last snapshot")
	fmt.Println(hr("═", 60))
	fmt.Printf("  Previous  : %s  (%d movies)\n", fmtTime(from["capturedAt"]), intVal(from["count"]))
	fmt.Printf("  Current   : %s  (%d movies)\n", fmtTime(to["capturedAt"]), intVal(to["count"]))
	sign := ""
	if net > 0 {
		sign = "+"
	}
	fmt.Printf("  Net change: %s%d\n", sign, net)
	fmt.Println(hr("─", 60))

	if len(added) == 0 {
		fmt.Println("\n  No new movies in this drop.")
	} else {
		fmt.Printf("\n  🆕  %d NEW MOVIE(S)\n\n", len(added))
		for _, raw := range added {
			fmt.Println(movieLine(mapVal(raw)))
		}
	}

	fmt.Println()

	if len(removed) > 0 {
		fmt.Println(hr("!", 60))
		fmt.Printf("  ⚠️  WARNING: %d MOVIE(S) WENT MISSING IN THIS DROP\n", len(removed))
		fmt.Println(hr("!", 60))
		fmt.Printf("  Between: %s  →  %s\n\n", fmtTime(from["capturedAt"]), fmtTime(to["capturedAt"]))
		for _, raw := range removed {
			fmt.Printf("  ✗ MISSING: %s\n", movieLine(mapVal(raw))[4:]) // strip leading "  • "
		}
		fmt.Println()
	}
}

func cmdMissing() {
	data, err := apiGet("/api/snapshots/missing")
	if err != nil {
		fatal(err)
	}
	events := sliceVal(data["missing"])

	fmt.Println()
	fmt.Println(hr("═", 60))
	fmt.Println("  MISSING MOVIE REPORT  —  all-time disappearances")
	fmt.Println(hr("═", 60))

	if len(events) == 0 {
		fmt.Print("\n  ✓ No movies have gone missing across any snapshot pair.\n\n")
		return
	}

	fmt.Printf("\n  ⚠️  %d disappearance event(s) found:\n\n", len(events))

	lastGone := ""
	for _, raw := range events {
		ev := mapVal(raw)
		movie := mapVal(ev["movie"])
		goneAt := fmtTime(ev["goneAt"])
		lastSeenAt := fmtTime(ev["lastSeenAt"])
		goneID := strVal(ev["goneId"])

		// Group by transition
		if goneID != lastGone {
			fmt.Println(hr("─", 60))
			fmt.Printf("  Disappeared between:\n")
			fmt.Printf("    Last seen : %s\n", lastSeenAt)
			fmt.Printf("    Gone in   : %s\n\n", goneAt)
			lastGone = goneID
		}
		fmt.Printf("    ✗ %s (%d)\n", strVal(movie["title"]), intVal(movie["year"]))
	}
	fmt.Println(hr("─", 60))
	fmt.Println()
}

func cmdDiff(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: snapshot-cli diff <from-id> <to-id>")
		os.Exit(1)
	}
	fromID, toID := args[0], args[1]
	data, err := apiGet(fmt.Sprintf("/api/snapshots/diff?from=%s&to=%s", fromID, toID))
	if err != nil {
		fatal(err)
	}
	diff := mapVal(data["diff"])
	from := mapVal(diff["from"])
	to := mapVal(diff["to"])
	added := sliceVal(diff["added"])
	removed := sliceVal(diff["removed"])
	net := intVal(diff["netChange"])

	fmt.Println()
	fmt.Println(hr("═", 60))
	fmt.Println("  SNAPSHOT DIFF")
	fmt.Println(hr("═", 60))
	fmt.Printf("  From  : %s  (%d movies)\n", fmtTime(from["capturedAt"]), intVal(from["count"]))
	fmt.Printf("  To    : %s  (%d movies)\n", fmtTime(to["capturedAt"]), intVal(to["count"]))
	sign := ""
	if net > 0 {
		sign = "+"
	}
	fmt.Printf("  Net   : %s%d\n", sign, net)
	fmt.Println(hr("─", 60))

	fmt.Printf("\n  🆕 Added (%d):\n", len(added))
	if len(added) == 0 {
		fmt.Println("     (none)")
	}
	for _, raw := range added {
		fmt.Println(movieLine(mapVal(raw)))
	}

	fmt.Printf("\n  ✗ Removed (%d):\n", len(removed))
	if len(removed) == 0 {
		fmt.Println("     (none)")
	}
	for _, raw := range removed {
		fmt.Println(movieLine(mapVal(raw)))
	}
	fmt.Println()
}

func cmdShow(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: snapshot-cli show <snapshot-id>")
		os.Exit(1)
	}
	id := args[0]
	data, err := apiGet("/api/snapshots/" + id)
	if err != nil {
		fatal(err)
	}
	snap := mapVal(data["snapshot"])
	movies := sliceVal(snap["movies"])

	fmt.Printf("\nSnapshot  : %s\n", strVal(snap["id"]))
	fmt.Printf("Captured  : %s\n", fmtTime(snap["capturedAt"]))
	fmt.Printf("Movies    : %d\n", intVal(snap["count"]))
	fmt.Println(hr("─", 50))
	for _, raw := range movies {
		fmt.Println(movieLine(mapVal(raw)))
	}
	fmt.Println()
}

// ── entry point ───────────────────────────────────────────────────────────────

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `snapshot-cli — Plex Movie Snapshot CLI

Server URL: %s  (override with PLEX_DASH_URL env var)

Commands:
  list            List all snapshots
  take            Take a new snapshot of your Plex library
  latest          Show new & missing movies since the last snapshot
  missing         Show all movies that have gone missing across all snapshots
  diff <from> <to> Diff two specific snapshots by ID
  show <id>        Print every movie in a snapshot

Examples:
  snapshot-cli take
  snapshot-cli latest
  snapshot-cli missing
  snapshot-cli list
  snapshot-cli diff 20260101-120000 20260110-090000
  PLEX_DASH_URL=http://192.168.1.10:8081 snapshot-cli latest

`, baseURL())
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "list":
		cmdList()
	case "take":
		cmdTake()
	case "latest":
		cmdLatest()
	case "missing":
		cmdMissing()
	case "diff":
		cmdDiff(rest)
	case "show":
		cmdShow(rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", cmd)
		usage()
		os.Exit(1)
	}
}
