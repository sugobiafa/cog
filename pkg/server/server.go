package server

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
)

// TODO(andreas): decouple saving zip files from image building into two separate API calls?
// TODO(andreas): separate targets for different CUDA versions? how does that change the yaml design?

const topLevelSourceDir = "source"

type Server struct {
	port               int
	webHooks           []*WebHook
	authDelegate       string
	db                 database.Database
	dockerImageBuilder docker.ImageBuilder
	servingPlatform    serving.Platform
	store              storage.Storage
}

func NewServer(port int, rawWebHooks []string, authDelegate string, db database.Database, dockerImageBuilder docker.ImageBuilder, servingPlatform serving.Platform, store storage.Storage) (*Server, error) {
	webHooks := []*WebHook{}
	for _, rawWebHook := range rawWebHooks {
		webHook, err := newWebHook(rawWebHook)
		if err != nil {
			return nil, err
		}
		webHooks = append(webHooks, webHook)
	}
	return &Server{
		port:               port,
		webHooks:           webHooks,
		authDelegate:       authDelegate,
		db:                 db,
		dockerImageBuilder: dockerImageBuilder,
		servingPlatform:    servingPlatform,
		store:              store,
	}, nil
}

func (s *Server) Start() error {
	router := mux.NewRouter()

	router.Path("/").
		Methods(http.MethodGet).
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("OK"))
		})
	router.Path("/ping").
		Methods(http.MethodGet).
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("pong"))
		})
	router.Path("/v1/repos/{user}/{name}/models/{id}.zip").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.DownloadModel))
	router.Path("/v1/repos/{user}/{name}/models/").
		Methods(http.MethodPut).
		HandlerFunc(s.checkWriteAccess(s.ReceiveFile))
	router.Path("/v1/repos/{user}/{name}/models/").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.ListModels))
	router.Path("/v1/repos/{user}/{name}/models/{id}").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.SendModelMetadata))
	router.Path("/v1/repos/{user}/{name}/models/{id}").
		Methods(http.MethodDelete).
		HandlerFunc(s.checkWriteAccess(s.DeleteModel))
	router.Path("/v1/repos/{user}/{name}/cache-hashes/").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(s.GetCacheHashes))
	router.Path("/v1/auth/display-token-url").
		Methods(http.MethodGet).
		HandlerFunc(s.GetDisplayTokenURL)
	router.Path("/v1/auth/verify-token").
		Methods(http.MethodPost).
		HandlerFunc(s.VerifyToken)
	router.Path("/v1/repos/{user}/{name}/check-read").
		Methods(http.MethodGet).
		HandlerFunc(s.checkReadAccess(nil))
	console.Infof("Server running on 0.0.0.0:%d", s.port)

	loggedRouter := handlers.LoggingHandler(os.Stdout, router)

	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), loggedRouter)
}

func getRepoVars(r *http.Request) (user string, name string, id string) {
	vars := mux.Vars(r)
	return vars["user"], vars["name"], vars["id"]
}
