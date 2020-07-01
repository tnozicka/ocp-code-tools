package serving

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/tnozicka/ocp-code-tools/pkg/serving/apihandlers"
)

func RegisterHandlers(m *mux.Router, staticDirPath string, ciConfigPath string) {
	m.PathPrefix("/static").Handler(http.FileServer(http.Dir(staticDirPath)))

	m.PathPrefix("/api/ci-config").HandlerFunc(apihandlers.ObjectHandler(apihandlers.GetCIConfig(ciConfigPath)))
	m.PathPrefix("/api/golang-versions").HandlerFunc(apihandlers.ObjectHandler(apihandlers.GetCIConfig(ciConfigPath)))
}
