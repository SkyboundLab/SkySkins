package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/mineatar-io/skin-render"
	"github.com/redis/go-redis/v9"
)

type MojangProfile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Properties []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"properties"`
}

type MojangSkin struct {
	Timestamp int64 `json:"timestamp"`
	ProfileID string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Textures struct {
		Skin struct {
			URL string `json:"url"`
		} `json:"SKIN"`
		Cape struct {
			URL string `json:"url"`
		} `json:"CAPE"`
	}
}

type DraslProfile struct {
	CapeURL           string `json:"capeUrl"`
	CreatedAt         string `json:"createdAt"`
	FallbackPlayer    string `json:"fallbackPlayer"`
	Name              string `json:"name"`
	NameLastChangedAt string `json:"nameLastChangedAt"`
	OfflineUUID       string `json:"offlineUuid"`
	SkinModel         string `json:"skinModel"`
	SkinURL           string `json:"skinUrl"`
	UserUUID          string `json:"userUuid"`
	UUID              string `json:"uuid"`
}

type DraslConfig struct {
	Token string
	URL   string
}

var (
    ctx = context.Background()
    client *redis.Client
	Drasl DraslConfig
	port string
)

func main() {
	godotenv.Load()

	port = os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	Drasl = DraslConfig{
		Token: os.Getenv("DRASL_TOKEN"),
		URL:   os.Getenv("DRASL_URL"),
	}

	address := os.Getenv("REDIS_ADDR")
    password := os.Getenv("REDIS_PASSWORD")
    database, err := strconv.Atoi(os.Getenv("REDIS_DB"))
    if err != nil {
        log.Fatalf("Invalid REDIS_DB value: %v", err)
		os.Exit(1)
    }

    client = redis.NewClient(&redis.Options{
        Addr: address,
        Password: password,
        DB: database,
    })

    if err := client.Ping(ctx).Err(); err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
		os.Exit(1)
    }

	router := mux.NewRouter()

	router.HandleFunc("/d/{id}", drasl)
	router.HandleFunc("/m/{id}", mojang)
	router.HandleFunc("/e/{name}", ely)
	router.HandleFunc("/a/{id}/{name}", all)

	http.Handle("/", router)

	fmt.Printf("Listening on port %s", port)

	http.ListenAndServe(":"+port, nil)
}

func mojang(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
    id := vars["id"]

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	key := "skin-avatar:" + id

	cached, err := client.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cached)
		return
	}

	response, err := http.Get(fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/profile/%s", id))
	if err != nil {
		http.Error(w, "Failed to fetch profile", http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	data := MojangProfile{}

	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(data.Properties[0].Value)
	if err != nil {
		http.Error(w, "Failed to decode base64", http.StatusInternalServerError)
		return
	}

	skin := MojangSkin{}

	err = json.Unmarshal(decoded, &skin)
	if err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	if skin.Textures.Skin.URL != "" {
		buf, err := render(skin.Textures.Skin.URL)
		if err != nil {
			http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = client.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
		if err != nil {
			log.Printf("Warning: failed to cache image: %v", err)
		}

		w.Header().Set("Content-Type", "image/png")
		w.Write(buf.Bytes())
	} else {
		http.Error(w, "No skin URL found", http.StatusNotFound)
		return
	}
}

func drasl(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
    id := vars["id"]

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	id = fmt.Sprintf("%s-%s-%s-%s-%s",
		id[0:8],
		id[8:12],
		id[12:16],
		id[16:20],
		id[20:32],
	)

	key := "skin-avatar:" + id

	cached, err := client.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cached)
		return
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/drasl/api/v2/players/%s", Drasl.URL, id), nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Authorization", "Bearer "+Drasl.Token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch profile", http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	profile := DraslProfile{}

	err = json.Unmarshal(body, &profile)
	if err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	buf, err := render(profile.SkinURL)
	if err != nil {
		http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = client.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(buf.Bytes())
}

func ely(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
    name := vars["name"]

	key := "skin-avatar:" + name

	cached, err := client.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cached)
		return
	}

	buf, err := render(fmt.Sprintf("http://skinsystem.ely.by/skins/%s.png", name))
	if err != nil {
		http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = client.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(buf.Bytes())
}

func all(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	name := vars["name"]

	endpoints := []string{
		fmt.Sprintf("http://localhost:%s/d/%s", port, id),
		fmt.Sprintf("http://localhost:%s/m/%s", port, id),
		fmt.Sprintf("http://localhost:%s/e/%s", port, name),
	}

	var resp *http.Response
	var err error

	for _, url := range endpoints {
		resp, err = http.Get(url)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			break
		}

		resp.Body.Close()
		resp = nil
	}

	if resp == nil {
		http.Error(w, "Failed to fetch target", http.StatusBadGateway)
		return
	}

	buffer, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(buffer)
}

func render(url string) (*bytes.Buffer, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("skin not found")
	}

	buffer, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	source, err := png.Decode(bytes.NewReader(buffer))
	if err != nil {
		return nil, err
	}

	bounds := source.Bounds()
	img := image.NewNRGBA(bounds)
	draw.Draw(img, bounds, source, bounds.Min, draw.Src)

	avatar := skin.RenderFace(img, skin.Options{
		Overlay: true,
		Scale: 96,
	})

	var buf bytes.Buffer
	if err := png.Encode(&buf, avatar); err != nil {
		return nil, fmt.Errorf("failed to encode image to PNG: %w", err)
	}

	return &buf, nil
}