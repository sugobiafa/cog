package server

import (
	"archive/zip"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/replicate/modelserver/pkg/global"
)

type Server struct {
	db         *DB
	cloudbuild *CloudBuild
	ai *AIPlatform
}

func NewServer() (*Server, error) {
	s := new(Server)
	return s, nil
}

func (s *Server) Start() error {
	var err error
	s.db, err = NewDB()
	if err != nil {
		return err
	}
	defer s.db.Close()

	s.cloudbuild, err = NewCloudBuild()
	if err != nil {
		return fmt.Errorf("Failed to connect to CloudBuild: %w", err)
	}

	s.ai, err = NewAIPlatform()
	if err != nil {
		return fmt.Errorf("Failed to connect to AI Platform: %w", err)
	}

	router := mux.NewRouter()
	router.Path("/upload").
		Methods("POST").
		HandlerFunc(s.ReceiveFile)
	fmt.Println("Starting")
	return http.ListenAndServe(fmt.Sprintf(":%d", global.Port), router)
}

func (s *Server) ReceiveFile(w http.ResponseWriter, r *http.Request) {
	dockerTag, err := s.ReceiveModel(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Pushed model to %s", dockerTag)
}

func (s *Server) ReceiveModel(r *http.Request) (string, error) {
	// max 5GB models
	if err := r.ParseMultipartForm(5 << 30); err != nil {
		return "", fmt.Errorf("Failed to parse request: %w", err)
	}
	file, header, err := r.FormFile("file")
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("Failed to compute hash: %w", err)
	}
	hash := fmt.Sprintf("%x", hasher.Sum(nil))

	reader, err := zip.NewReader(file, header.Size)
	if err != nil {
		return "", fmt.Errorf("Failed to read zip file: %w", err)
	}

	parentDir, err := os.MkdirTemp("/tmp", "unzip")
	if err != nil {
		return "", fmt.Errorf("Failed to make tempdir: %w", err)
	}
	dir := filepath.Join(parentDir, topLevelSourceDir)
	if err := os.Mkdir(dir, 0755); err != nil {
		return "", fmt.Errorf("Failed to make source dir: %w", err)
	}
	if err := Unzip(reader, dir); err != nil {
		return "", fmt.Errorf("Failed to unzip: %w", err)
	}

	configRaw, err := os.ReadFile(filepath.Join(dir, "replicate.yaml"))
	if err != nil {
		return "", fmt.Errorf("Failed to read config yaml: %w", err)
	}
	config := new(Config)
	if err := yaml.Unmarshal(configRaw, config); err != nil {
		return "", fmt.Errorf("Failed to parse config yaml: %w", err)
	}

	dockerTag := "us-central1-docker.pkg.dev/replicate/andreas-scratch/" + config.Name + ":" + hash

	if err := s.cloudbuild.Submit(dir, hash, dockerTag, config.Dockerfile.Cpu); err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}

	if err := s.ai.Deploy(dockerTag, hash); err != nil {
		return "", err
	}

	// TODO: test model

	log.Info("Uploading to GCS")
	gcsPath := config.Name + "/" + hash + ".zip"
	if err := UploadToStorage(file, gcsPath); err != nil {
		return "", fmt.Errorf("Failed to upload to storage: %w", err)
	}

	log.Info("Inserting into database")
	if err := s.db.InsertModel(config.Name, hash, gcsPath, dockerTag); err != nil {
		return "", fmt.Errorf("Failed to insert into database: %w", err)
	}

	return dockerTag, nil
}
