package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "time"
	"bytes"
	"image"
	"image/png"

    "github.com/joho/godotenv"
    "github.com/redis/go-redis/v9"

	mineatar "github.com/mineatar-io/skin-render"
)

var (
    ctx         = context.Background()
    redisClient *redis.Client
)

func main() {
    err := godotenv.Load()
    if err != nil {
        log.Println("Warning: no .env file loaded, relying on environment variables")
    }

    redisAddr := os.Getenv("REDIS_ADDR")
    redisPassword := os.Getenv("REDIS_PASSWORD")
    redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
    if err != nil {
        log.Fatalf("Invalid REDIS_DB value: %v", err)
    }

    redisClient = redis.NewClient(&redis.Options{
        Addr:     redisAddr,
        Password: redisPassword,
        DB:       redisDB,
    })

    if err := redisClient.Ping(ctx).Err(); err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }


    http.HandleFunc("/r/ely/", handleRenderEly)
    http.HandleFunc("/r/mojang/", handleRenderMojang)
    http.HandleFunc("/r/all/", handleRenderAll)

    fmt.Println("API running at http://localhost:8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

// --- ENDPOINT HANDLERS ---

func handleRenderMojang(w http.ResponseWriter, r *http.Request) {
	uuid := strings.TrimPrefix(r.URL.Path, "/r/mojang/")
	uuid = normalizeUUID(uuid)
	if uuid == "" {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}
	handleRender(w, uuid, fetchSkinMojang)
}

func handleRenderEly(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/r/ely/")
	username = strings.TrimSpace(username)
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}
	handleRender(w, username, func(string) (string, func(), error) {
		return fetchSkinEly(username)
	})
}

func handleRenderAll(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/r/all/"), "/")
	if len(parts) != 2 {
		http.Error(w, "Expected format: /r/all/{uuid}/{username}", http.StatusBadRequest)
		return
	}
	uuid := normalizeUUID(parts[0])
	username := parts[1]
	if uuid == "" || username == "" {
		http.Error(w, "UUID and username are required", http.StatusBadRequest)
		return
	}

	err := handleRenderWithFallback(w, uuid,
		fetchSkinMojang,
		func(string) (string, func(), error) {
			return fetchSkinEly(username)
		},
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}

// --- SHARED RENDERING LOGIC ---

type fetchSkinFunc func(uuid string) (string, func(), error)

func handleRender(w http.ResponseWriter, cacheKey string, fetchFunc fetchSkinFunc) {
	cacheKey = "skin-face:" + cacheKey

	cachedPNG, err := redisClient.Get(ctx, cacheKey).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(cachedPNG)
		return
	}

	skinPath, cleanup, err := fetchFunc(cacheKey)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		http.Error(w, "Failed to fetch skin: "+err.Error(), http.StatusNotFound)
		return
	}

	faceImgBuf, err := RenderFace(skinPath)
	if err != nil {
		http.Error(w, "Failed to render face: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = redisClient.Set(ctx, cacheKey, faceImgBuf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(faceImgBuf.Bytes())
}

func handleRenderWithFallback(w http.ResponseWriter, uuid string, primary, fallback fetchSkinFunc) error {
	skinPath, cleanup, err := primary(uuid)
	if err == nil {
		if cleanup != nil {
			defer cleanup()
		}
		return writeAndCache(w, uuid, skinPath)
	}

	skinPath, cleanup, err = fallback(uuid)
	if err != nil {
		return errors.New("skin not found on both Mojang and Ely")
	}
	if cleanup != nil {
		defer cleanup()
	}
	return writeAndCache(w, uuid, skinPath)
}

func writeAndCache(w http.ResponseWriter, cacheKey, skinPath string) error {
	faceImgBuf, err := RenderFace(skinPath)
	if err != nil {
		return err
	}

	err = redisClient.Set(ctx, "skin-face:"+cacheKey, faceImgBuf.Bytes(), 48*time.Hour).Err()
	if err != nil {
		log.Printf("Warning: failed to cache image: %v", err)
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(faceImgBuf.Bytes())
	return nil
}

// --- FETCHERS ---

func fetchSkinEly(username string) (string, func(), error) {
	skinURL := fmt.Sprintf("http://skinsystem.ely.by/skins/%s.png", username)
	return fetchSkinFromURL(skinURL, username)
}

func fetchSkinMojang(uuid string) (string, func(), error) {
	skinURL, err := getMojangSkinURL(uuid)
	if err != nil {
		return "", nil, err
	}
	return fetchSkinFromURL(skinURL, uuid)
}

func fetchSkinFromURL(url, key string) (string, func(), error) {
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return "", nil, fmt.Errorf("failed to fetch skin from %s", url)
	}
	defer resp.Body.Close()

	tempDir := os.TempDir()
	skinPath := filepath.Join(tempDir, fmt.Sprintf("skin-%s.png", key))
	outFile, err := os.Create(skinPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create file: %w", err)
	}
	_, err = io.Copy(outFile, resp.Body)
	outFile.Close()
	if err != nil {
		os.Remove(skinPath)
		return "", nil, fmt.Errorf("failed to write skin: %w", err)
	}

	cleanup := func() {
		os.Remove(skinPath)
	}
	return skinPath, cleanup, nil
}

// --- MOJANG SESSION SERVER PARSE ---

func getMojangSkinURL(uuid string) (string, error) {
	url := fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/profile/%s", uuid)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("profile not found")
	}

	var data struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&data); err != nil {
		return "", err
	}

	for _, prop := range data.Properties {
		if prop.Name == "textures" {
			decoded, err := base64.StdEncoding.DecodeString(prop.Value)
			if err != nil {
				return "", err
			}
			var texData struct {
				Textures struct {
					Skin struct {
						URL string `json:"url"`
					} `json:"SKIN"`
				} `json:"textures"`
			}
			if err := json.Unmarshal(decoded, &texData); err != nil {
				return "", err
			}
			if texData.Textures.Skin.URL != "" {
				return texData.Textures.Skin.URL, nil
			}
		}
	}

	return "", errors.New("skin URL not found")
}

// --- UTILITIES ---

func normalizeUUID(s string) string {
	cleaned := strings.ReplaceAll(s, "-", "")
	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, cleaned)
	if !matched {
		return ""
	}
	return strings.ToLower(cleaned)
}

func RenderFace(skinPath string) (*bytes.Buffer, error) {
    f, err := os.Open(skinPath)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    img, err := png.Decode(f)
    if err != nil {
        return nil, err
    }

    nrgbaImg := image.NewNRGBA(img.Bounds())
    for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
        for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
            nrgbaImg.Set(x, y, img.At(x, y))
        }
    }

    face := mineatar.RenderFace(nrgbaImg, mineatar.Options{
		Overlay: true,
        Scale: 96,
    })

    var buf bytes.Buffer
    err = png.Encode(&buf, face)
    if err != nil {
        return nil, err
    }

    return &buf, nil
}
