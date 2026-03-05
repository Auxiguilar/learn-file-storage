package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
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

	bytes := make([]byte, 32)
	rand.Read(bytes)
	encodedString := hex.EncodeToString(bytes)

	ext, _ := strings.CutPrefix(mediaType, "video/") // fuck this shit

	key := fmt.Sprintf("%s.%s", encodedString, ext)

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
