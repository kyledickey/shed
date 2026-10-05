package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// VerifySignature reports whether header, the X-Hub-Signature-256 value of a
// webhook delivery, is the valid HMAC-SHA256 of body under secret.
func VerifySignature(secret string, body []byte, header string) bool {
	hexSum, ok := strings.CutPrefix(header, "sha256=")
	if !ok || secret == "" {
		return false
	}
	got, err := hex.DecodeString(hexSum)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// PushEvent is the part of a push webhook payload shed acts on.
type PushEvent struct {
	// Repo is the repository as owner/name.
	Repo string
	// Branch is the pushed branch, or empty if the ref is not a branch.
	Branch string
	// SHA is the new head commit.
	SHA string
	// Message is the first line of the head commit's message.
	Message string
	// Author is the head commit's author name.
	Author string
	// Deleted reports that the push deleted the branch.
	Deleted bool
}

// ParsePush decodes the body of a push webhook delivery.
func ParsePush(body []byte) (PushEvent, error) {
	var p struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		HeadCommit *struct {
			ID      string `json:"id"`
			Message string `json:"message"`
			Author  struct {
				Name     string `json:"name"`
				Username string `json:"username"`
			} `json:"author"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return PushEvent{}, fmt.Errorf("github: parse push event: %w", err)
	}
	ev := PushEvent{Repo: p.Repository.FullName, SHA: p.After, Deleted: p.Deleted}
	if branch, ok := strings.CutPrefix(p.Ref, "refs/heads/"); ok {
		ev.Branch = branch
	}
	if hc := p.HeadCommit; hc != nil {
		if ev.SHA == "" {
			ev.SHA = hc.ID
		}
		ev.Message = firstLine(hc.Message)
		ev.Author = hc.Author.Name
		if ev.Author == "" {
			ev.Author = hc.Author.Username
		}
	}
	return ev, nil
}
