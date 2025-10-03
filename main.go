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
	"github.com/robfig/cron/v3"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
	ID         string `bson:"id" json:"id"`
	Name       string `bson:"name" json:"name"`
	URL        string `bson:"url" json:"url"`
	Properties []struct {
		Name      string `bson:"name" json:"name"`
		Signature string `bson:"signature,omitempty" json:"signature,omitempty"`
		Value     string `bson:"value" json:"value"`
	} `bson:"properties" json:"properties"`
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
    ctx = context.Background()
    redisClient *redis.Client
	mongoClient *mongo.Client
	draslConfig DraslConfig
	mineskin string
	port string
)

func main() {
	godotenv.Load()

	port = os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	draslConfig = DraslConfig{
		Token: os.Getenv("DRASL_TOKEN"),
		URL:   os.Getenv("DRASL_URL"),
	}

	mineskin = os.Getenv("MINESKIN_TOKEN")

	address := os.Getenv("REDIS_ADDR")
    password := os.Getenv("REDIS_PASSWORD")
    database, err := strconv.Atoi(os.Getenv("REDIS_DB"))
    if err != nil {
        log.Fatalf("Invalid REDIS_DB value: %v", err)
		os.Exit(1)
    }

    redisClient = redis.NewClient(&redis.Options{
        Addr: address,
        Password: password,
        DB: database,
    })

    if err := redisClient.Ping(ctx).Err(); err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
		os.Exit(1)
    }

	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		log.Fatal("MONGODB_URI not set")
		os.Exit(1)
	}

	mongoClient, err = mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := mongoClient.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()

	router := mux.NewRouter()

	router.HandleFunc("/d/{id}", drasl)
	router.HandleFunc("/m/{id}", mojang)
	router.HandleFunc("/e/{id}", ely)
	router.HandleFunc("/a/{id}", all)

	router.HandleFunc("/textures/signed/{id}", textures)

	http.Handle("/", router)

	fmt.Printf("Listening on port %s", port)

	c := cron.New()

	c.AddFunc("@daily", updateSkins)

	c.Start()

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

	key := "skin:avatar:" + id

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
		buf, err := render(skin.Textures.Skin.URL)
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

	key := "skin:avatar:" + id

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

	buf, err := render(profile.SkinURL)
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
	vars := mux.Vars(r)
    id := vars["id"]

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
		return
	}

	key := "skin:avatar:" + id

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

	username := usernames[len(usernames) - 1].Name

	buf, err := render(fmt.Sprintf("http://skinsystem.ely.by/skins/%s.png", username))
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
	vars := mux.Vars(r)
	id := vars["id"]

	endpoints := []string{
		fmt.Sprintf("http://localhost:%s/d/%s", port, id),
		fmt.Sprintf("http://localhost:%s/m/%s", port, id),
		fmt.Sprintf("http://localhost:%s/e/%s", port, id),
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

func textures(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	id = strings.ReplaceAll(id, "-", "")

	matched, _ := regexp.MatchString(`^[0-9a-fA-F]{32}$`, id)
	if !matched {
		http.Error(w, "Invalid UUID", http.StatusBadRequest)
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
	collection := mongoClient.Database("SkySkins").Collection("drasl")

	var body []byte
	err = collection.FindOne(ctx, bson.M{"id": id}).Decode(&result)

	if err == nil {
		body, err = json.Marshal(result)
		if err != nil {
			http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
			return
		}
	} else {
		// response, err := http.Get(fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/profile/%s", id))
		// if err != nil {
		// 	http.Error(w, "Failed to fetch profile", http.StatusInternalServerError)
		// 	return
		// }
		// defer response.Body.Close()

		// if response.StatusCode != 200 {
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

			body, err = io.ReadAll(response.Body)
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

			username := usernames[len(usernames) - 1].Name

			response, err = http.Get(fmt.Sprintf("http://skinsystem.ely.by/textures/signed/%s", username))
			if err != nil {
				http.Error(w, "Failed to fetch profile", http.StatusInternalServerError)
				return
			}
			defer response.Body.Close()

			if response.StatusCode != 200 {
				http.Error(w, "Profile not found", http.StatusNotFound)
				return
			}
		// }

		body, err = io.ReadAll(response.Body)
		if err != nil {
			http.Error(w, "Failed to read response body", http.StatusInternalServerError)
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

	collection := mongoClient.Database("SkySkins").Collection("drasl")

	for _, user := range users {
		log.Printf("Checking %s", user.Name)

		id := strings.ReplaceAll(user.UUID, "-", "")

		var result DraslSkin

		err = collection.FindOne(ctx, bson.M{"id": id}).Decode(&result)
		if err != nil && err != mongo.ErrNoDocuments {
			log.Printf("Warning: DB find error for %s: %v", user.Name, err)
			continue
		}

		if err == nil && result.URL == user.SkinURL {
			log.Printf("Skipping %s - URL unchanged", user.Name)
			continue
		}

		value, signature, skinError := uploadSkin(user)
		if skinError != nil || value == "" || signature == "" {
			log.Printf("Warning: failed to upload skin for %s: %v", user.Name, err)
			continue
		}

		doc := bson.M{
			"id":   id,
			"name": user.Name,
			"url":  user.SkinURL,
			"properties": []bson.M{
				{
					"name":      "textures",
					"value":     value,
					"signature": signature,
				},
				{
					"name":  "drasl",
					"value": "we do not want to be drasl!",
				},
			},
		}

		if err == mongo.ErrNoDocuments {
			log.Printf("Inserting %s in DB: %v", user.Name, doc)
			_, err = collection.InsertOne(ctx, doc)
		} else {
			log.Printf("Updating %s in DB: %v", user.Name, doc)
			_, err = collection.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": doc})
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