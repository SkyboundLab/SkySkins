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

	infisical "github.com/infisical/go-sdk"
	"github.com/mineatar-io/skin-render"
	"github.com/redis/go-redis/v9"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MojangProfile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Properties []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"properties"`
}

type MojangSkin struct {
	Timestamp   int64  `json:"timestamp"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Textures    struct {
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

type DraslUser struct {
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

type DraslSkin struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Properties []struct {
		Name      string `json:"name"`
		Signature string `json:"signature,omitempty"`
		Value     string `json:"value"`
	} `json:"properties"`
}

type DraslConfig struct {
	Token string
	URL   string
}

type ElyUser struct {
	Name        string `json:"name"`
	ChangedToAt *int64 `json:"changedToAt,omitempty"`
}

type MineSkin struct {
	Skin struct {
		Texture struct {
			Data struct {
				Value     string `json:"value"`
				Signature string `json:"signature"`
			} `json:"data"`
		} `json:"texture"`
	} `json:"skin"`
}

var (
	ctx         = context.Background()
	redisClient *redis.Client
	db          *pgxpool.Pool
	draslConfig DraslConfig
	mineskin    string
	port        string
)

func loadEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func loadSecrets() {
	clientID := os.Getenv("INFISICAL_CLIENT_ID")
	clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
	projectID := os.Getenv("INFISICAL_PROJECT_ID")
	if clientID == "" || clientSecret == "" || projectID == "" {
		return
	}

	client := infisical.NewInfisicalClient(ctx, infisical.Config{
		SiteUrl: getEnv("INFISICAL_SITE_URL", "https://app.infisical.com"),
	})

	if _, err := client.Auth().UniversalAuthLogin(clientID, clientSecret); err != nil {
		log.Printf("Warning: Infisical authentication failed: %v", err)
		return
	}

	environment := getEnv("INFISICAL_ENVIRONMENT", "prod")
	secretKeys := []string{
		"DATABASE_URL",
		"REDIS_ADDR",
		"REDIS_PASSWORD",
		"REDIS_DB",
		"DRASL_TOKEN",
		"DRASL_URL",
		"MINESKIN_TOKEN",
	}

	for _, key := range secretKeys {
		if os.Getenv(key) != "" {
			continue
		}
		secret, err := client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
			SecretKey:   key,
			Environment: environment,
			ProjectID:   projectID,
			SecretPath:  "/",
		})
		if err != nil {
			log.Printf("Warning: failed to fetch secret %s from Infisical: %v", key, err)
			continue
		}
		os.Setenv(key, secret.SecretValue)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	loadEnv()
	loadSecrets()

	port = getEnv("PORT", "8080")

	draslConfig = DraslConfig{
		Token: getEnv("DRASL_TOKEN", ""),
		URL:   getEnv("DRASL_URL", ""),
	}

	mineskin = getEnv("MINESKIN_TOKEN", "")

	address := getEnv("REDIS_ADDR", "localhost:6379")
	password := getEnv("REDIS_PASSWORD", "")
	database, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		log.Fatalf("Invalid REDIS_DB value: %v", err)
		os.Exit(1)
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     address,
		Password: password,
		DB:       database,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
		os.Exit(1)
	}

	uri := getEnv("DATABASE_URL", "")
	if uri == "" {
		log.Fatal("DATABASE_URL not set")
	}

	db, err = pgxpool.New(ctx, uri)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS drasl (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			url        TEXT NOT NULL,
			properties JSONB NOT NULL DEFAULT '[]'::jsonb
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /d/{id}", drasl)
	mux.HandleFunc("GET /m/{id}", mojang)
	mux.HandleFunc("GET /e/{id}", ely)
	mux.HandleFunc("GET /a/{id}", all)

	mux.HandleFunc("GET /textures/signed/{id}", textures)

	fmt.Printf("Listening on port %s", port)

	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(next))
			updateSkins()
		}
	}()

	http.ListenAndServe(":"+port, mux)
}

func mojang(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	size := 96
	if sizeParam := r.URL.Query().Get("size"); sizeParam != "" {
		if parsedSize, err := strconv.Atoi(sizeParam); err == nil && parsedSize > 0 && parsedSize <= 512 {
			size = parsedSize
		}
	}

	key := fmt.Sprintf("skin:avatar:%s:%d", id, size)

	cached, err := redisClient.Get(ctx, key).Bytes()
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
		buf, err := render(skin.Textures.Skin.URL, size)
		if err != nil {
			http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = redisClient.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
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
	id := r.PathValue("id")

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

	size := 96
	if sizeParam := r.URL.Query().Get("size"); sizeParam != "" {
		if parsedSize, err := strconv.Atoi(sizeParam); err == nil && parsedSize > 0 && parsedSize <= 512 {
			size = parsedSize
		}
	}

	key := fmt.Sprintf("skin:avatar:%s:%d", id, size)

	cached, err := redisClient.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cached)
		return
	}

	request, err := http.NewRequest("GET", fmt.Sprintf("%s/drasl/api/v2/players/%s", draslConfig.URL, id), nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	request.Header.Set("Authorization", "Bearer "+draslConfig.Token)

	response, err := http.DefaultClient.Do(request)
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

	buf, err := render(profile.SkinURL, size)
	if err != nil {
		http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = redisClient.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(buf.Bytes())
}

func ely(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	size := 96
	if sizeParam := r.URL.Query().Get("size"); sizeParam != "" {
		if parsedSize, err := strconv.Atoi(sizeParam); err == nil && parsedSize > 0 && parsedSize <= 512 {
			size = parsedSize
		}
	}

	key := fmt.Sprintf("skin:avatar:%s:%d", id, size)

	cached, err := redisClient.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cached)
		return
	}

	response, err := http.Get(fmt.Sprintf("https://authserver.ely.by/api/user/profiles/%s/names", id))
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

	var usernames []ElyUser

	err = json.Unmarshal([]byte(body), &usernames)
	if err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	username := usernames[len(usernames)-1].Name

	buf, err := render(fmt.Sprintf("http://skinsystem.ely.by/skins/%s.png", username), size)
	if err != nil {
		http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = redisClient.Set(ctx, key, buf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(buf.Bytes())
}

func all(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	size := r.URL.Query().Get("size")
	query := ""
	if size != "" {
		query = "?size=" + size
	}

	endpoints := []string{
		fmt.Sprintf("http://localhost:%s/d/%s%s", port, id, query),
		fmt.Sprintf("http://localhost:%s/m/%s%s", port, id, query),
		fmt.Sprintf("http://localhost:%s/e/%s%s", port, id, query),
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

func render(url string, size int) (*bytes.Buffer, error) {
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
		Scale:   size,
	})

	var buf bytes.Buffer
	if err := png.Encode(&buf, avatar); err != nil {
		return nil, fmt.Errorf("failed to encode image to PNG: %w", err)
	}

	return &buf, nil
}

func close(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent) // 204
	if hj, ok := w.(http.Hijacker); ok {
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
			return
		}
	}
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func textures(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		close(w)
		return
	}

	key := "skin:data:" + id

	cached, err := redisClient.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}

	var result DraslSkin
	var props []byte

	var body []byte
	err = db.QueryRow(ctx, "SELECT id, name, url, properties FROM drasl WHERE id = $1", id).Scan(&result.ID, &result.Name, &result.URL, &props)
	if err == nil {
		json.Unmarshal(props, &result.Properties)
	}

	if err == nil {
		body, err = json.Marshal(result)
		if err != nil {
			close(w)
			return
		}
	} else {
		response, err := http.Get(fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/profile/%s?unsigned=false", id))
		if err != nil {
			close(w)
			return
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			response, err = http.Get(fmt.Sprintf("https://authserver.ely.by/api/user/profiles/%s/names", id))
			if err != nil {
				close(w)
				return
			}
			defer response.Body.Close()

			if response.StatusCode != 200 {
				close(w)
				return
			}

			body, err = io.ReadAll(response.Body)
			if err != nil {
				close(w)
				return
			}

			var usernames []ElyUser

			err = json.Unmarshal([]byte(body), &usernames)
			if err != nil {
				close(w)
				return
			}

			username := usernames[len(usernames)-1].Name

			response, err = http.Get(fmt.Sprintf("http://skinsystem.ely.by/textures/signed/%s", username))
			if err != nil {
				close(w)
				return
			}
			defer response.Body.Close()

			if response.StatusCode != 200 {
				close(w)
				return
			}
		}

		body, err = io.ReadAll(response.Body)
		if err != nil {
			close(w)
			return
		}
	}

	err = redisClient.Set(ctx, key, body, 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache skin data for %s: %v", id, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func updateSkins() {
	request, err := http.NewRequest("GET", fmt.Sprintf("%s/drasl/api/v2/players", draslConfig.URL), nil)
	if err != nil {
		log.Printf("Warning: failed to create request: %v", err)
		return
	}

	request.Header.Set("Authorization", "Bearer "+draslConfig.Token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Printf("Warning: failed to fetch profile: %v", err)
		return
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		log.Printf("Warning: failed to fetch profile: %v", err)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("Warning: failed to read response body: %v", err)
		return
	}

	var users []DraslUser

	err = json.Unmarshal([]byte(body), &users)
	if err != nil {
		log.Printf("Warning: failed to parse JSON: %v", err)
		return
	}

	for _, user := range users {
		log.Printf("Checking %s", user.Name)

		id := strings.ReplaceAll(user.UUID, "-", "")

		var url string
		var exists bool
		err = db.QueryRow(ctx, "SELECT url FROM drasl WHERE id = $1", id).Scan(&url)
		if err == nil {
			exists = true
		} else if err != pgx.ErrNoRows {
			log.Printf("Warning: DB find error for %s: %v", user.Name, err)
			continue
		}

		if exists && url == user.SkinURL {
			log.Printf("Skipping %s - URL unchanged", user.Name)
			continue
		}

		value, signature, skinError := uploadSkin(user)
		if skinError != nil || value == "" || signature == "" {
			log.Printf("Warning: failed to upload skin for %s: %v", user.Name, skinError)
			continue
		}

		props, _ := json.Marshal([]map[string]interface{}{
			{"name": "textures", "value": value, "signature": signature},
			{"name": "drasl", "value": "we do not want to be drasl!"},
		})

		if !exists {
			_, err = db.Exec(ctx, "INSERT INTO drasl (id, name, url, properties) VALUES ($1, $2, $3, $4)", id, user.Name, user.SkinURL, props)
			if err == nil {
				log.Printf("Inserted %s in DB", user.Name)
			}
		} else {
			_, err = db.Exec(ctx, "UPDATE drasl SET name = $2, url = $3, properties = $4 WHERE id = $1", id, user.Name, user.SkinURL, props)
			if err == nil {
				log.Printf("Updated %s in DB", user.Name)
			}
		}

		if err != nil {
			log.Printf("Warning: failed to insert/update DB for %s: %v", user.Name, err)
		}
	}
}

func uploadSkin(user DraslUser) (value, signature string, err error) {
	payload := map[string]string{
		"variant":    user.SkinModel,
		"name":       user.Name,
		"visibility": "public",
		"url":        user.SkinURL,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	request, err := http.NewRequest("POST", "https://api.mineskin.org/v2/generate", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", "", err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+mineskin)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()

	log.Printf("Uploading %s", user.Name)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", "", err
	}

	log.Printf("Response: %s", string(body))

	var skin MineSkin
	if err := json.Unmarshal(body, &skin); err != nil {
		return "", "", err
	}

	return skin.Skin.Texture.Data.Value, skin.Skin.Texture.Data.Signature, nil
}
