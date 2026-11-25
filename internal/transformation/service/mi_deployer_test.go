/*
 * Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMI is a minimal stand-in for MI's Management API: login, upload a CAR,
// delete a CAR, list APIs. Uploaded CARs are opened to check their contents.
type fakeMI struct {
	mu       sync.Mutex
	apps     map[string]string            // application file name -> API name
	apis     map[string]string            // API name -> URL
	carFiles map[string]map[string]string // application file name -> path -> content
	deleted  []string
}

func newFakeMI() *fakeMI {
	return &fakeMI{apps: map[string]string{}, apis: map[string]string{}, carFiles: map[string]map[string]string{}}
}

func (f *fakeMI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/management")
	if path != "/login" && r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/login":
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "admin" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"AccessToken":"test-token"}`))

	case r.Method == http.MethodPost && path == "/applications":
		file, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		contents := map[string]string{}
		apiName, context := "", ""
		for _, zf := range reader.File {
			rc, _ := zf.Open()
			text, _ := io.ReadAll(rc)
			rc.Close()
			contents[zf.Name] = string(text)
			if strings.Contains(string(text), "<api ") {
				apiName = between(string(text), `name="`, `"`)
				context = between(string(text), `context="`, `"`)
			}
		}
		app := strings.TrimSuffix(header.Filename, ".car")
		f.apps[app] = apiName
		f.carFiles[app] = contents
		f.apis[apiName] = "http://localhost:8290" + context
		_, _ = w.Write([]byte(`{"Message":"Successfully added Carbon Application ` + header.Filename + `"}`))

	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/applications/"):
		app := strings.TrimPrefix(path, "/applications/")
		apiName, ok := f.apps[app]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.apps, app)
		delete(f.apis, apiName)
		f.deleted = append(f.deleted, app)

	case r.Method == http.MethodGet && path == "/apis":
		type api struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		list := []api{}
		for name, url := range f.apis {
			list = append(list, api{name, url})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": len(list), "list": list})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	if j := strings.Index(s, end); j >= 0 {
		return s[:j]
	}
	return ""
}

func TestDeployToMI(t *testing.T) {

	mi := newFakeMI()
	server := httptest.NewTLSServer(mi)
	defer server.Close()
	cfg := miConfig{ManagementURL: server.URL + "/management", Username: "admin", Password: "admin"}
	template := `{"traits":{"full_name":"${payload.guestName}","primary_email":"${payload.email}"}}`

	// First deployment: version 1.0.0.
	url, err := deployToMI(context.Background(), cfg, "seaview", "t2-booking-1", "1.0.0", template, "")
	if err != nil {
		t.Fatalf("deploy 1.0.0: %v", err)
	}
	if url != "http://localhost:8290/transform/seaview/t2-booking-1" {
		t.Fatalf("unexpected URL %q", url)
	}

	files := mi.carFiles["seaview-t2-booking-1_1.0.0"]
	apiXML := files["T_seaview_t2-booking-1_1.0.0/T_seaview_t2-booking-1-1.0.0.xml"]
	if !strings.Contains(apiXML, `context="/transform/seaview/t2-booking-1"`) {
		t.Errorf("API XML has the wrong context:\n%s", apiXML)
	}
	if !strings.Contains(apiXML, "<format>"+template+"</format>") {
		t.Errorf("API XML does not carry the template unchanged:\n%s", apiXML)
	}
	if !strings.Contains(files["artifacts.xml"], `name="seaview-t2-booking-1" version="1.0.0"`) {
		t.Errorf("artifacts.xml has the wrong application name or version:\n%s", files["artifacts.xml"])
	}

	// Update: version 1.0.1 replaces 1.0.0.
	if _, err := deployToMI(context.Background(), cfg, "seaview", "t2-booking-1", "1.0.1", template, "1.0.0"); err != nil {
		t.Fatalf("deploy 1.0.1: %v", err)
	}
	if len(mi.deleted) != 1 || mi.deleted[0] != "seaview-t2-booking-1_1.0.0" {
		t.Errorf("expected 1.0.0 to be removed first, removed: %v", mi.deleted)
	}
	if _, ok := mi.apps["seaview-t2-booking-1_1.0.1"]; !ok {
		t.Errorf("1.0.1 was not uploaded; apps: %v", mi.apps)
	}
}

func TestDeployToMIWrongPassword(t *testing.T) {

	server := httptest.NewTLSServer(newFakeMI())
	defer server.Close()
	cfg := miConfig{ManagementURL: server.URL + "/management", Username: "admin", Password: "wrong"}

	_, err := deployToMI(context.Background(), cfg, "seaview", "t1", "1.0.0", `{}`, "")
	if err == nil || !strings.Contains(err.Error(), "MI login answered HTTP 401") {
		t.Fatalf("expected a login error, got %v", err)
	}
}

func TestXMLText(t *testing.T) {
	if got := xmlText(`{"a":"${payload.x}","b":"x & y < z"}`); got != `{"a":"${payload.x}","b":"x &amp; y &lt; z"}` {
		t.Errorf("unexpected escaping: %s", got)
	}
}
