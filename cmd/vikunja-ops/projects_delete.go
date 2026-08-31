package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

func runProjectsDelete(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printProjectsDeleteUsage(stdout)
		return 0
	}
	if len(args) == 0 || deleteDuplicateOrUnsafeFlags(args[1:]) {
		printProjectsDeleteUsage(stderr)
		return 2
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		printProjectsDeleteUsage(stderr)
		return 2
	}
	f := flag.NewFlagSet("projects delete", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	apply := f.Bool("apply", false, "execute")
	confirm := f.String("confirm", "", "confirmation")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		printProjectsDeleteUsage(stderr)
		return 2
	}
	confirmSet := false
	f.Visit(func(x *flag.Flag) { confirmSet = confirmSet || x.Name == "confirm" })
	if (*apply && !confirmSet) || (!*apply && confirmSet) || (*apply && !validDeleteToken(*confirm)) {
		printProjectsDeleteUsage(stderr)
		return 2
	}
	cfg, e := config.Load()
	if e != nil {
		fmt.Fprintf(stderr, "projects delete: %v\n", e)
		return 1
	}
	api := client.New(nil)
	s, e := api.ScanProjectDelete(context.Background(), cfg.BaseURL, cfg.Token, id)
	if e != nil {
		fmt.Fprintln(stderr, "projects delete: safety scan did not qualify scope; no delete sent")
		return 1
	}
	impact := deleteImpactHash(s)
	token := deleteToken(s, impact)
	if !*apply {
		return writeJSON("projects delete", map[string]any{"mode": "preview", "operation": "projects_delete", "root_project_id": id, "descendant_ids": s.Descendants, "projects": s.Projects, "tasks": s.Tasks, "views": s.Views, "buckets": s.Buckets, "impact_digest": "sha256:" + impact, "confirmation_token": token, "counts": map[string]int{"projects": len(s.Projects), "tasks": len(s.Tasks), "views": len(s.Views), "buckets": len(s.Buckets)}, "scope": "qualified caller-observable scope; non-atomic; server may have hidden cascades", "next_args": []string{"projects", "delete", strconv.FormatInt(id, 10), "--apply", "--confirm", token}}, stdout, stderr)
	}
	if *confirm != token {
		fmt.Fprintln(stderr, "projects delete: confirmation does not match current qualified snapshot; no delete sent")
		return 1
	}
	deleteErr := api.DeleteProject(context.Background(), cfg.BaseURL, cfg.Token, id)
	// A delete transport attempt is always followed by exactly one independent
	// root readback, even when the DELETE status itself was not acceptable.
	status, readErr := api.ReadDeletedProject(context.Background(), cfg.BaseURL, cfg.Token, id)
	if deleteErr != nil || readErr != nil || status != 404 {
		fmt.Fprintln(stderr, "projects delete: delete outcome is unknown or not accepted")
		return 1
	}
	return writeJSON("projects delete", map[string]any{"mode": "apply", "operation": "projects_delete", "root_project_id": id, "delete_status": "204 acknowledged", "readback_status": "GET currently not readable (404)", "caveat": "readback does not prove causation or independently verify cascades"}, stdout, stderr)
}
func deleteDuplicateOrUnsafeFlags(args []string) bool {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		n := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if n != "apply" && n != "confirm" {
			return true
		}
		if seen[n] {
			return true
		}
		seen[n] = true
		if n == "confirm" && !strings.Contains(a, "=") {
			i++
		}
	}
	return false
}
func validDeleteToken(s string) bool {
	if !strings.HasPrefix(s, "sha256:") || len(s) != 71 {
		return false
	}
	for _, r := range s[7:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func deleteHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func deleteImpactHash(s client.ProjectDeleteSnapshot) string {
	v := struct {
		Projects []client.DeleteProject `json:"projects"`
		Tasks    []client.DeleteTask    `json:"tasks"`
		Views    []client.DeleteView    `json:"views"`
		Buckets  []client.DeleteBucket  `json:"buckets"`
	}{s.Projects, s.Tasks, s.Views, s.Buckets}
	return deleteHash(v)
}
func deleteToken(s client.ProjectDeleteSnapshot, impact string) string {
	v := struct {
		Version   int     `json:"version"`
		Operation string  `json:"operation"`
		Root      int64   `json:"root"`
		Desc      []int64 `json:"descendants"`
		Instance  string  `json:"instance"`
		Impact    string  `json:"impact"`
		DefaultID int64   `json:"default_project_id"`
	}{1, "projects_delete", s.RootID, s.Descendants, s.Instance, impact, s.DefaultProjectID}
	return "sha256:" + deleteHash(v)
}
