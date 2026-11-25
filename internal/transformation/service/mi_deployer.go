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

// Part M: deploys a transformation to MI from Go, through MI's Management API.
// It does what ~/transformer-poc/deploy.sh used to do, so CDS no longer needs
// any script outside the repository:
//
//  1. build the API XML and the two descriptor files from the template,
//  2. zip them into a CAR (in memory),
//  3. log in to the Management API,
//  4. remove the previous version of the CAR, if any,
//  5. upload the new CAR,
//  6. wait until MI lists the API and return its URL.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults match a local MI started with ./micro-integrator.sh.
const (
	defaultMIManagementURL = "https://localhost:9164/management"
	defaultMIUsername      = "admin"
	defaultMIPassword      = "admin"
	miCarVersion           = "1.0.0" // version of the API inside the CAR; the CAR itself carries the transformation version
	miPollInterval         = time.Second
	miUndeployTimeout      = 20 * time.Second
	miDeployTimeout        = 30 * time.Second
)

// miConfig is where and how CDS reaches MI's Management API.
type miConfig struct {
	ManagementURL string
	Username      string
	Password      string
}

// miConfigFromEnv reads MI_MANAGEMENT_URL, MI_USERNAME and MI_PASSWORD, with
// the local defaults when they are not set.
func miConfigFromEnv() miConfig {
	return miConfig{
		ManagementURL: strings.TrimRight(envOr("MI_MANAGEMENT_URL", defaultMIManagementURL), "/"),
		Username:      envOr("MI_USERNAME", defaultMIUsername),
		Password:      envOr("MI_PASSWORD", defaultMIPassword),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// miManagementClient talks to MI's Management API. PoC only: MI ships with a
// self-signed certificate, so certificate checks are skipped here; production
// would trust MI's certificate instead.
var miManagementClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // #nosec G402 -- PoC, local MI
}

// miNames are the names MI uses for one deployed transformation endpoint.
type miNames struct {
	API     string // T_seaview_t2-booking-1
	Context string // /transform/seaview/t2-booking-1
	App     string // seaview-t2-booking-1 (the CAR's application name)
}

func namesFor(org, name string) miNames {
	return miNames{
		API:     "T_" + org + "_" + name,
		Context: "/transform/" + org + "/" + name,
		App:     org + "-" + name,
	}
}

// deployToMI deploys one endpoint (a template under /transform/<org>/<name>)
// and returns the URL MI reports for it. oldVersion, when set, is removed first,
// because two APIs cannot share one context.
func deployToMI(ctx context.Context, cfg miConfig, org, name, version, template, oldVersion string) (string, error) {

	names := namesFor(org, name)

	car, err := buildCAR(names, version, template)
	if err != nil {
		return "", fmt.Errorf("building the CAR: %w", err)
	}
	token, err := miLogin(ctx, cfg)
	if err != nil {
		return "", err
	}
	if oldVersion != "" {
		if err := miUndeploy(ctx, cfg, token, names, oldVersion); err != nil {
			return "", err
		}
	}
	if err := miUpload(ctx, cfg, token, names.App+"_"+version+".car", car); err != nil {
		return "", err
	}
	return miWaitForAPI(ctx, cfg, token, names.API)
}

// buildCAR returns a CAR (a zip) holding the API, its artifact.xml and the
// application's artifacts.xml: the same three files as in Parts B and C.
func buildCAR(names miNames, version, template string) ([]byte, error) {

	apiFolder := names.API + "_" + miCarVersion
	apiFile := names.API + "-" + miCarVersion + ".xml"

	files := []struct{ path, content string }{
		{"artifacts.xml", `<?xml version="1.0" encoding="UTF-8"?>
<artifacts>
    <artifact name="` + names.App + `" version="` + version + `" type="carbon/application">
        <dependency artifact="` + names.API + `" version="` + miCarVersion + `" include="true" serverRole="EnterpriseIntegrator"/>
    </artifact>
</artifacts>
`},
		{apiFolder + "/artifact.xml", `<?xml version="1.0" encoding="UTF-8"?>
<artifact name="` + names.API + `" groupId="com.cds.transformers" version="` + miCarVersion + `" type="synapse/api" serverRole="EnterpriseIntegrator">
    <file>` + apiFile + `</file>
</artifact>
`},
		{apiFolder + "/" + apiFile, `<?xml version="1.0" encoding="UTF-8"?>
<api xmlns="http://ws.apache.org/ns/synapse" name="` + names.API + `" context="` + names.Context + `">
    <resource methods="POST" uri-template="/">
        <inSequence>
            <payloadFactory media-type="json" template-type="default">
                <format>` + xmlText(template) + `</format>
            </payloadFactory>
            <respond/>
        </inSequence>
        <faultSequence/>
    </resource>
</api>
`},
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	for _, file := range files {
		writer, err := zipWriter.Create(file.path)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(file.content)); err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// xmlText escapes the characters that are not allowed in XML text. Quotes and
// ${...} expressions pass through unchanged, as in the hand-written template.
func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// miLogin returns an access token for the Management API (Part A4's get_token).
func miLogin(ctx context.Context, cfg miConfig) (string, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ManagementURL+"/login", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(cfg.Username, cfg.Password)
	body, status, err := miDo(req)
	if err != nil {
		return "", fmt.Errorf("MI login: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("MI login answered HTTP %d: %s", status, tail(string(body), 300))
	}
	var login struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.Unmarshal(body, &login); err != nil || login.AccessToken == "" {
		return "", fmt.Errorf("MI login returned no AccessToken: %s", tail(string(body), 300))
	}
	return login.AccessToken, nil
}

// miUndeploy removes the CAR of a previous version and waits until its API is
// gone. A CAR that is already gone (404) is fine.
func miUndeploy(ctx context.Context, cfg miConfig, token string, names miNames, oldVersion string) error {

	app := names.App + "_" + oldVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		cfg.ManagementURL+"/applications/"+url.PathEscape(app), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, status, err := miDo(req)
	if err != nil {
		return fmt.Errorf("removing %s from MI: %w", app, err)
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return fmt.Errorf("removing %s from MI answered HTTP %d: %s", app, status, tail(string(body), 300))
	}

	deadline := time.Now().Add(miUndeployTimeout)
	for time.Now().Before(deadline) {
		apiURL, err := miFindAPI(ctx, cfg, token, names.API)
		if err != nil {
			return err
		}
		if apiURL == "" {
			return nil
		}
		if err := sleep(ctx, miPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("MI still lists %s %d seconds after removing %s", names.API,
		int(miUndeployTimeout.Seconds()), app)
}

// miUpload uploads a CAR as multipart form field "file" (Part D1's curl --form).
func miUpload(ctx context.Context, cfg miConfig, token, fileName string, car []byte) error {

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	if _, err := part.Write(car); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ManagementURL+"/applications", &form)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	body, status, err := miDo(req)
	if err != nil {
		return fmt.Errorf("uploading %s to MI: %w", fileName, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("uploading %s to MI answered HTTP %d: %s", fileName, status, tail(string(body), 300))
	}
	return nil
}

// miWaitForAPI polls the Management API until MI lists the API, then returns
// its URL (Part D3). MI deploys an uploaded CAR within a few seconds.
func miWaitForAPI(ctx context.Context, cfg miConfig, token, apiName string) (string, error) {

	deadline := time.Now().Add(miDeployTimeout)
	for time.Now().Before(deadline) {
		apiURL, err := miFindAPI(ctx, cfg, token, apiName)
		if err != nil {
			return "", err
		}
		if apiURL != "" {
			return apiURL, nil
		}
		if err := sleep(ctx, miPollInterval); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("MI did not list %s within %d seconds; check the MI log for a deployment error",
		apiName, int(miDeployTimeout.Seconds()))
}

// miFindAPI returns the URL of the named API, or "" if MI does not list it.
func miFindAPI(ctx context.Context, cfg miConfig, token, apiName string) (string, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ManagementURL+"/apis", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, status, err := miDo(req)
	if err != nil {
		return "", fmt.Errorf("listing MI APIs: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("listing MI APIs answered HTTP %d: %s", status, tail(string(body), 300))
	}
	var apis struct {
		List []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"list"`
	}
	if err := json.Unmarshal(body, &apis); err != nil {
		return "", fmt.Errorf("reading the MI API list: %w", err)
	}
	for _, api := range apis.List {
		if api.Name == apiName {
			return api.URL, nil
		}
	}
	return "", nil
}

// miDo sends a request and returns the body (at most 1 MB) and the status.
func miDo(req *http.Request) ([]byte, int, error) {
	resp, err := miManagementClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, err
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
