package handlers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/ghodss/yaml"
	"github.com/tnozicka/ocp-code-tools/pkg/api"
	"k8s.io/klog/v2"
)

var (
	releaseToGolangVersions = map[string]string{
		"4.1": "1.12",
		"4.2": "1.12",
		"4.3": "1.12",
		"4.4": "1.13",
		"4.5": "1.13",
		"4.6": "1.13",
	}
)

func GolangVersionsHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawData, err := ioutil.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Always unmarshal to make sure the file is valid
		c := &api.Config{}
		err = yaml.Unmarshal(rawData, c)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		switch r.Header.Get("Accept") {
		case "application/json":
			data, err := json.MarshalIndent(c, "", "  ")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, err = w.Write(data)
			if err != nil {
				klog.Error(err)
			}

		default:
			fallthrough
		case "application/yaml":
			data, err := yaml.Marshal(c)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
			_, err = w.Write(data)
			if err != nil {
				klog.Error(err)
			}
		}
	}
}
