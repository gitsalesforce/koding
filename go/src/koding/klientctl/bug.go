package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"time"

	"koding/kites/config"
	"koding/klient/uploader"
	konfig "koding/klientctl/config"
	"koding/klientctl/endpoint/machine"
	"koding/klientctl/helper"

	"github.com/codegangsta/cli"
	"github.com/koding/logging"
	"github.com/skratchdot/open-golang/open"
)

//go:generate $GOPATH/bin/go-bindata -mode 420 -modtime 1475345133 -pkg main -prefix ../../../../.github/ -o issue.md.go ../../../../.github/ISSUE_TEMPLATE.md

type BugMetadata struct {
	KiteID      string                  `json:"kiteID"`
	Konfig      *config.Konfig          `json:"konfig"`
	Version     int                     `json:"version"`
	Environment string                  `json:"environment"`
	Files       []*machine.UploadedFile `json:"files"`
	CreatedAt   time.Time               `json:"createdAt"`
}

func (bm *BugMetadata) ToFile() *machine.UploadedFile {
	p, err := jsonMarshal(bm)
	if err != nil {
		panic(err)
	}

	hash := sha1.Sum(p)

	return &machine.UploadedFile{
		File:    "bug/" + hex.EncodeToString(hash[:]) + ".json",
		Content: p,
	}
}

func Bug(_ *cli.Context, log logging.Logger, _ string) (int, error) {
	meta := metadata()

	resp, err := machine.ListMount(&machine.ListMountOptions{})
	if err != nil {
		return 1, err
	}

	for id, mounts := range resp {
		for _, m := range mounts {
			opts := &machine.InspectMountOptions{
				Identifier: string(m.ID),
				Sync:       true,
				Log:        log.New("bug"),
			}

			records, err := machine.InspectMount(opts)
			if err != nil {
				return 1, err
			}

			if len(records.Sync) > 0 {
				p, err := json.Marshal(records.Sync)
				if err != nil {
					return 1, err
				}

				meta.Files = append(meta.Files, &machine.UploadedFile{
					File:    "sync/" + id + "/" + string(m.ID) + ".json",
					Content: p,
				})
			}
		}
	}

	// Best-effort attempt of uploading system files
	// for troubleshooting.
	_ = machine.UploadForce(meta.Files...)

	report := meta.ToFile()

	if err := machine.Upload(report); err != nil {
		return 1, err
	}

	s := signature(report.URL)
	t := ""

	if u, ok := askScreencast(); ok {
		t = fmt.Sprintf(asciicastBody, u, s)
	} else {
		t = fmt.Sprintf(issueBody, s)
	}

	if err := open.Start("https://github.com/koding/koding/issues/new?body=" + url.QueryEscape(t)); err != nil {
		return 1, err
	}

	fmt.Println(s)

	return 0, nil
}

func metadata() *BugMetadata {
	// Strip KiteKey from config.
	cfg := *konfig.Konfig
	cfg.KiteKey = ""

	return &BugMetadata{
		KiteID:      konfig.Konfig.KiteConfig().Id,
		Konfig:      &cfg,
		Version:     konfig.VersionNum(),
		Environment: konfig.Environment,
		Files:       systemFiles(),
		CreatedAt:   time.Now(),
	}
}

func systemFiles() (files []*machine.UploadedFile) {
	for _, log := range uploader.LogFiles {
		if _, err := os.Stat(log); os.IsNotExist(err) {
			continue
		}

		files = append(files, &machine.UploadedFile{
			File: log,
		})
	}

	return files
}

func jsonMarshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func signature(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		panic(err)
	}

	return hex.EncodeToString([]byte(u.Path))
}

func askScreencast() (*url.URL, bool) {
	if _, err := exec.LookPath("asciinema"); err != nil {
		return nil, false
	}

	link, err := helper.Ask("Did you record your shell with asciinema? If yes, please provide a URL of the recording [none]: ")
	if err != nil {
		return nil, false
	}

	if link == "" {
		return nil, false
	}

	u, err := url.Parse(link)
	if err != nil || u.Host != "asciinema.org" {
		fmt.Fprintln(os.Stderr, "Invalid asciinema link, ignoring.")
		return nil, false
	}

	return u, true
}

const sign = "\n### Signature\n```\n%s\n```\n"

var (
	issueBody     = string(append(MustAsset("ISSUE_TEMPLATE.md"), []byte(sign)...))
	asciicastBody = "<!--- Provide a general summary of the issue in the Title above -->\n\n## Steps to reproduce\n[![asciicast](%[1]s.png)](%[1]s)\n" + sign
)
