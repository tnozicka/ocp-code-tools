package apihandlers

import (
	"encoding/json"
	"net/http"

	"github.com/ghodss/yaml"
	"k8s.io/klog/v2"
)

func ObjectHandler(o interface{}, err error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		switch r.Header.Get("Accept") {
		case "application/json":
			data, err := json.MarshalIndent(o, "", "  ")
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
			data, err := yaml.Marshal(o)
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
