package remotecontent

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSchemaMethods(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":1,"methods":[{"label":"BTC","destination":"wallet-info","qr_payload":"","action":"copy","active":true,"qr":true},{"label":"skip","destination":"x","active":false},{"label":"site","destination":"http://unsafe","action":"open","active":true}]}`)
	}))
	defer s.Close()
	p := (Fetcher{Endpoint: s.URL, Client: s.Client()}).loadMethods(context.Background(), "donations")
	if p.Err != nil || len(p.Methods) != 1 || p.Methods[0].QRPayload != "wallet-info" {
		t.Fatalf("%+v", p)
	}
}
func TestEdition(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"edition":"frosty","about":{"top":"One\nTwo","bottom":"Ending"},"howto_links":[{"text":"Guide","url":"https://example.com"},{"text":"No","url":"javascript:alert(1)"}]}`)
	}))
	defer s.Close()
	p := (Fetcher{Endpoint: s.URL, Client: s.Client()}).loadEdition(context.Background())
	if p.Err != nil || p.About.Top != "One\nTwo" || len(p.Guides) != 1 {
		t.Fatalf("%+v", p)
	}
}
func TestImagePNG(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 20, 20))
	im.Set(0, 0, color.RGBA{1, 2, 3, 255})
	w := httptest.NewRecorder()
	png.Encode(w, im)
	if _, _, e := image.Decode(w.Body); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPS(t *testing.T) {
	if HTTPS("javascript:hi") || HTTPS("http://example.com") || !HTTPS("https://example.com/path") {
		t.Fatal("validation")
	}
}

func TestStage16BannerAndIndependentCalls(t *testing.T) {
	// The fake HTTPS server returns the real Stage16 public JSON schema. Banner
	// artwork is an actual PNG, while Contacts intentionally fails independently.
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	mux.HandleFunc("/ads.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":3,"slots":[{"id":"main_middle","sponsors":[]},{"id":"main_top","sponsors":[{"id":"zugbonk-house","title":"House","image":%q,"website":"https://zugbonk.com"}]}]}`, server.URL+"/banners/test.png")
	})
	mux.HandleFunc("/banners/test.png", func(w http.ResponseWriter, r *http.Request) {
		im := image.NewRGBA(image.Rect(0, 0, 20, 20))
		im.Set(0, 0, color.RGBA{1, 2, 3, 255})
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, im)
	})
	mux.HandleFunc("/donations.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":1,"methods":[{"label":"Support","destination":"https://example.org","action":"open","qr":true,"active":true}]}`)
	})
	mux.HandleFunc("/contacts.json", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) })
	mux.HandleFunc("/v1/edition-content", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"edition":"frosty","about":{"top":"Hello","bottom":"World"},"howto_links":[]}`)
	})
	f := Fetcher{Endpoint: server.URL, Client: server.Client()}
	parts := f.FetchAll(context.Background())
	if len(parts) != 4 {
		t.Fatal("missing responses")
	}
	if parts[0].Err != nil || parts[0].Image == nil || parts[0].Banner.Title != "House" {
		t.Fatalf("banner: %+v", parts[0])
	}
	if parts[1].Err != nil || len(parts[1].Methods) != 1 || parts[1].Methods[0].QRPayload != "https://example.org" {
		t.Fatalf("donations: %+v", parts[1])
	}
	if parts[2].Err == nil {
		t.Fatal("failed contacts request must be reported independently")
	}
	if parts[3].Err != nil || parts[3].About.Bottom != "World" {
		t.Fatalf("edition: %+v", parts[3])
	}
}

func TestExplicitQRPayload(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":1,"methods":[{"label":"Crypto","destination":"address-label","qr_payload":"real-address","action":"copy","qr":true}]}`)
	}))
	defer s.Close()
	p := (Fetcher{Endpoint: s.URL, Client: s.Client()}).loadMethods(context.Background(), "contacts")
	if p.Err != nil || len(p.Methods) != 1 || p.Methods[0].QRPayload != "real-address" {
		t.Fatalf("custom payload lost: %+v", p)
	}
}
