package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30) // why does it have to be so damn esoteric??

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't fetch video metadata", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not the video author", err)
		return
	}

	data, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "FormFile error", err)
		return
	}
	defer data.Close()

	// ????
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil || mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", nil)
		return
	}

	f, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close() // LIFO

	_, err = io.Copy(f, data)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy temp file", err)
		return
	}

	f.Seek(0, io.SeekStart)

	aspectRatio, err := getVideoAspectRatio(f.Name())
	videoType := ""

	switch aspectRatio {
	case "16:9":
		videoType = "landscape"
	case "9:16":
		videoType = "portrait"
	default:
		videoType = "other"
	}

	log.Printf("Aspect ratio: %s\nVideo type: %s", aspectRatio, videoType)

	bytes := make([]byte, 32)
	rand.Read(bytes)
	encodedString := hex.EncodeToString(bytes)

	ext, _ := strings.CutPrefix(mediaType, "video/") // fuck this shit

	key := fmt.Sprintf("%s/%s.%s", videoType, encodedString, ext)

	putParams := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Body:        f,
		ContentType: &mediaType,
		Key:         &key,
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &putParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Coudn't put file in bucket", err)
		return
	}

	// https://<bucket-name>.s3.<region>.amazonaws.com/<key>
	videoURL := fmt.Sprintf(
		"https://%s.s3.%s.amazonaws.com/%s",
		cfg.s3Bucket, cfg.s3Region, key,
	)

	videoMetadata.VideoURL = &videoURL

	// update video
	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command(
		"ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath,
	)

	// I just wanna check if it's all good...
	if cmd.Err != nil {
		return "", cmd.Err // seems about right to me!
	}

	buf := bytes.Buffer{}
	cmd.Stdout = &buf

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	params := struct {
		S []struct {
			Width  float32 `json:"width"`
			Height float32 `json:"height"`
		} `json:"streams"`
	}{}

	err = json.Unmarshal(buf.Bytes(), &params)
	if err != nil {
		return "", err
	}

	log.Printf("Width: %.0f\nHeight: %.0f\n", params.S[0].Width, params.S[0].Height)

	if ratio := int(params.S[0].Width / params.S[0].Height * 9); ratio == 16 {
		return "16:9", nil
	} else if ratio := int(params.S[0].Width / params.S[0].Height * 16); ratio == 9 {
		return "9:16", nil
	}

	return "other", nil
}
