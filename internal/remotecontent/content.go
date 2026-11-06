// Package remotecontent implements the optional read-only Stage 16 public API.
// All requests are bounded, independent and HTTPS-only. No identifiers, usage
// reports, activation checks, permissions or telemetry are sent.
package remotecontent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const ProductionEndpoint = "https://api.wow-app.zugbonk.com"
const maxJSON = 256 * 1024
const maxImage = 900000

type Banner struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Website     string `json:"website"`
}
type Method struct {
	Label       string `json:"label"`
	Destination string `json:"destination"`
	QRPayload   string `json:"qr_payload"`
	Action      string `json:"action"`
	Active      bool   `json:"active"`
	QR          bool   `json:"qr"`
}
type Guide struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
type About struct {
	Top    string `json:"top"`
	Bottom string `json:"bottom"`
}
type Partial struct {
	Kind    string
	Banner  Banner
	Image   image.Image
	Methods []Method
	About   About
	Guides  []Guide
	Err     error
}
type Fetcher struct {
	Endpoint string
	Client   *http.Client
}

func HTTPS(s string) bool {
	u, e := url.Parse(strings.TrimSpace(s))
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}
func approvedImageURL(s, host string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.User == nil && u.Host == host
}
func (f Fetcher) base() string {
	if f.Endpoint != "" {
		return strings.TrimSuffix(f.Endpoint, "/")
	}
	return ProductionEndpoint
}
func (f Fetcher) httpClient() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}
func (f Fetcher) get(ctx context.Context, path string, cap int) ([]byte, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", f.base()+path, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Accept", "application/json")
	resp, e := f.httpClient().Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, int64(cap)+1))
	if e != nil {
		return nil, e
	}
	if len(data) > cap {
		return nil, errors.New("response too large")
	}
	return data, nil
}
func (f Fetcher) loadBanner(ctx context.Context) Partial {
	p := Partial{Kind: "banner"}
	data, e := f.get(ctx, "/ads.json", maxJSON)
	if e != nil {
		p.Err = e
		return p
	}
	var doc struct {
		Slots []struct {
			ID       string `json:"id"`
			Sponsors []struct {
				ID string `json:"id"`
				Banner
			} `json:"sponsors"`
		} `json:"slots"`
	}
	if e = json.Unmarshal(data, &doc); e != nil {
		p.Err = e
		return p
	}
	for _, s := range doc.Slots {
		if s.ID == "main_top" {
			for _, v := range s.Sponsors {
				if v.ID == "zugbonk-house" {
					p.Banner = v.Banner
					if !HTTPS(p.Banner.Website) {
						p.Banner.Website = ""
					}
					// Stage 16 serves the mutable artwork only from the same public API host.
					base, _ := url.Parse(f.base())
					if !approvedImageURL(p.Banner.Image, base.Host) {
						p.Err = errors.New("untrusted banner image host")
						return p
					}
					imageURL, _ := url.Parse(p.Banner.Image)
					req, e := http.NewRequestWithContext(ctx, "GET", imageURL.String(), nil)
					if e != nil {
						p.Err = e
						return p
					}
					resp, e := f.httpClient().Do(req)
					if e != nil {
						p.Err = e
						return p
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						p.Err = fmt.Errorf("banner HTTP %d", resp.StatusCode)
						return p
					}
					imageBytes, e := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
					if e != nil || len(imageBytes) > maxImage {
						p.Err = errors.New("invalid banner size")
						return p
					}
					cfg, _, e := image.DecodeConfig(bytes.NewReader(imageBytes))
					if e != nil || cfg.Width > 1600 || cfg.Height > 1800 || cfg.Width < 10 || cfg.Height < 10 {
						p.Err = errors.New("invalid image dimensions")
						return p
					}
					im, _, e := image.Decode(bytes.NewReader(imageBytes))
					if e != nil {
						p.Err = e
						return p
					}
					if im.Bounds().Dx() > 1600 || im.Bounds().Dy() > 1800 || im.Bounds().Dx() < 10 || im.Bounds().Dy() < 10 {
						p.Err = errors.New("invalid image dimensions")
						return p
					}
					p.Image = im
					return p
				}
			}
		}
	}
	p.Err = errors.New("main_top banner not configured")
	return p
}
func safeText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return ""
	}
	for _, c := range s {
		if c < 32 && c != '\n' && c != '\t' && c != '\r' {
			return ""
		}
	}
	return s
}
func (f Fetcher) loadMethods(ctx context.Context, kind string) Partial {
	p := Partial{Kind: kind}
	data, e := f.get(ctx, "/"+kind+".json", maxJSON)
	if e != nil {
		p.Err = e
		return p
	}
	var doc struct {
		Version int `json:"version"`
		Methods []struct {
			Label       string `json:"label"`
			Destination string `json:"destination"`
			QRPayload   string `json:"qr_payload"`
			Action      string `json:"action"`
			Active      *bool  `json:"active"`
			QR          bool   `json:"qr"`
		} `json:"methods"`
	}
	if e = json.Unmarshal(data, &doc); e != nil {
		p.Err = e
		return p
	}
	if doc.Version != 1 {
		p.Err = errors.New("unsupported methods schema")
		return p
	}
	for _, m := range doc.Methods {
		if m.Active != nil && !*m.Active {
			continue
		}
		label, destination := safeText(m.Label, 140), safeText(m.Destination, 900)
		if label == "" || destination == "" {
			continue
		}
		action := m.Action
		if action != "open" {
			action = "copy"
		}
		if action == "open" && !HTTPS(destination) {
			continue
		}
		qr := m.QR
		payload := safeText(m.QRPayload, 900)
		if payload == "" {
			payload = destination
		}
		if len(payload) > 319 {
			qr = false
		}
		p.Methods = append(p.Methods, Method{Label: label, Destination: destination, QRPayload: payload, Action: action, Active: true, QR: qr})
	}
	return p
}
func (f Fetcher) loadEdition(ctx context.Context) Partial {
	p := Partial{Kind: "edition"}
	data, e := f.get(ctx, "/v1/edition-content?edition=frosty", maxJSON)
	if e != nil {
		p.Err = e
		return p
	}
	var doc struct {
		OK      bool    `json:"ok"`
		Edition string  `json:"edition"`
		About   About   `json:"about"`
		Guides  []Guide `json:"howto_links"`
	}
	if e = json.Unmarshal(data, &doc); e != nil {
		p.Err = e
		return p
	}
	if !doc.OK || doc.Edition != "frosty" {
		p.Err = errors.New("invalid edition response")
		return p
	}
	p.About = About{Top: safeText(doc.About.Top, 4000), Bottom: safeText(doc.About.Bottom, 4000)}
	for _, g := range doc.Guides {
		g.Text = safeText(g.Text, 140)
		if g.Text != "" && HTTPS(g.URL) {
			p.Guides = append(p.Guides, g)
		}
	}
	return p
}

// FetchAll returns independent results, allowing one broken endpoint to leave
// other previously accepted content untouched. Network I/O happens off UI thread.
func (f Fetcher) FetchAll(ctx context.Context) []Partial {
	out := make([]Partial, 4)
	jobs := []func(context.Context) Partial{f.loadBanner, func(c context.Context) Partial { return f.loadMethods(c, "donations") }, func(c context.Context) Partial { return f.loadMethods(c, "contacts") }, f.loadEdition}
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job func(context.Context) Partial) { defer wg.Done(); out[i] = job(ctx) }(i, job)
	}
	wg.Wait()
	return out
}
