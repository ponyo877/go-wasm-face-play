//go:build js && wasm
// +build js,wasm

package detector

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"syscall/js"

	pigo "github.com/esimov/pigo/core"
)

// FlpCascade holds the binary representation of the facial landmark points cascade files
type FlpCascade struct {
	*pigo.PuplocCascade
	error
}

const perturb = 63

var (
	cascade          []byte
	puplocCascade    []byte
	faceClassifier   *pigo.Pigo
	puplocClassifier *pigo.PuplocCascade
	flpcs            map[string][]*FlpCascade
	imgParams        *pigo.ImageParams
	err              error
)

var (
	eyeCascades  = []string{"lp46", "lp44", "lp42", "lp38", "lp312"}
	mouthCascade = []string{"lp93", "lp84", "lp82", "lp81"}
)

// Detector struct holds the main components of the fetching operation.
type Detector struct {
	respChan chan []uint8
	errChan  chan error
	done     chan struct{}

	window js.Value
}

// NewDetector initializes a new constructor function.
func NewDetector() *Detector {
	var d Detector
	d.window = js.Global()

	return &d
}

// FetchCascade retrive the cascade file through a JS http connection.
// It should return the binary data as uint8 integers or err in case of an error.
func (d *Detector) FetchCascade(url string) ([]byte, error) {
	d.respChan = make(chan []uint8)
	d.errChan = make(chan error)

	promise := js.Global().Call("fetch", url)
	promise.Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			response := args[0]
			if !response.Get("ok").Bool() {
				errorMsg := response.Get("statusText").String()
				d.errChan <- errors.New(errorMsg)
			}
		}()
		return nil
	}))
	success := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		response := args[0]
		response.Call("arrayBuffer").Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			go func() {
				buffer := args[0]
				uint8Array := js.Global().Get("Uint8Array").New(buffer)

				jsbuf := make([]byte, uint8Array.Get("length").Int())
				js.CopyBytesToGo(jsbuf, uint8Array)
				d.respChan <- jsbuf
			}()
			return nil
		}))
		return nil
	})

	failure := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go func() {
			err := fmt.Errorf("unable to fetch the cascade file: %s", args[0].String())
			d.errChan <- err
		}()
		return nil
	})

	promise.Call("then", success, failure)

	select {
	case resp := <-d.respChan:
		return resp, nil
	case err := <-d.errChan:
		return nil, err
	}
}

// ParseCascade loads and parse the cascade file through the
// Javascript `location.href` method, using the `js/syscall` package.
// It will return the cascade file encoded into a byte array.
func (d *Detector) ParseCascade(path string) ([]byte, error) {
	href := js.Global().Get("location").Get("href")
	u, err := url.Parse(href.String())
	if err != nil {
		return nil, err
	}
	u.Path = path

	resp, err := http.Get(u.String())
	if err != nil || resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("%v cascade file is missing", u.String()))
	}
	defer resp.Body.Close()

	buffer, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	uint8Array := js.Global().Get("Uint8Array").New(len(buffer))
	js.CopyBytesToJS(uint8Array, buffer)

	buff := make([]byte, uint8Array.Get("length").Int())
	js.CopyBytesToGo(buff, uint8Array)

	return buff, nil
}

// Log calls the `console.log` Javascript function
func (d *Detector) Log(args ...interface{}) {
	d.window.Get("console").Call("log", args...)
}

// UnpackCascades unpack all of used cascade files.
func (d *Detector) UnpackCascades() error {
	p := pigo.NewPigo()

	cascade, err = d.ParseCascade("/cascade/facefinder")
	if err != nil {
		return errors.New("error reading the facefinder cascade file")
	}
	// Unpack the binary file. This will return the number of cascade trees,
	// the tree depth, the threshold and the prediction from tree's leaf nodes.
	faceClassifier, err = p.Unpack(cascade)
	if err != nil {
		return errors.New("error unpacking the facefinder cascade file")
	}

	plc := pigo.NewPuplocCascade()

	puplocCascade, err = d.ParseCascade("/cascade/puploc")
	if err != nil {
		return errors.New("error reading the puploc cascade file")
	}

	puplocClassifier, err = plc.UnpackCascade(puplocCascade)
	if err != nil {
		return errors.New("error unpacking the puploc cascade file")
	}

	flpcs, err = d.parseFlpCascades("/cascade/lps/")
	if err != nil {
		return errors.New("error unpacking the facial landmark points detection cascades")
	}
	return nil
}

// DetectFaces runs the cluster detection over the webcam frame
// received as a pixel array and returns the detected faces.
func (d *Detector) DetectFaces(pixels []uint8, width, height int) [][]int {
	results := d.clusterDetection(pixels, width, height)
	dets := make([][]int, len(results))

	for i := 0; i < len(results); i++ {
		dets[i] = append(dets[i], results[i].Row, results[i].Col, results[i].Scale, int(results[i].Q))
	}
	return dets
}

// DetectLeftPupil detects the left pupil
func (d *Detector) DetectLeftPupil(results []int) *pigo.Puploc {
	puploc := &pigo.Puploc{
		Row:      results[0] - int(0.085*float32(results[2])),
		Col:      results[1] - int(0.185*float32(results[2])),
		Scale:    float32(results[2]) * 0.4,
		Perturbs: perturb,
	}
	leftEye := puplocClassifier.RunDetector(*puploc, *imgParams, 0.0, false)
	if leftEye.Row > 0 && leftEye.Col > 0 {
		return leftEye
	}
	return nil
}

// DetectRightPupil detects the right pupil
func (d *Detector) DetectRightPupil(results []int) *pigo.Puploc {
	puploc := &pigo.Puploc{
		Row:      results[0] - int(0.085*float32(results[2])),
		Col:      results[1] + int(0.185*float32(results[2])),
		Scale:    float32(results[2]) * 0.4,
		Perturbs: perturb,
	}
	rightEye := puplocClassifier.RunDetector(*puploc, *imgParams, 0.0, false)
	if rightEye.Row > 0 && rightEye.Col > 0 {
		return rightEye
	}
	return nil
}

// DetectLandmarkPoints detects the landmark points
func (d *Detector) DetectLandmarkPoints(leftEye, rightEye *pigo.Puploc) [][]int {
	var (
		det = make([][]int, 15)
		idx int
	)

	for _, eye := range eyeCascades {
		for _, flpc := range flpcs[eye] {
			flp := flpc.GetLandmarkPoint(leftEye, rightEye, *imgParams, perturb, false)
			if flp.Row > 0 && flp.Col > 0 {
				det[idx] = append(det[idx], flp.Col, flp.Row, int(flp.Scale))
			}
			idx++

			flp = flpc.GetLandmarkPoint(leftEye, rightEye, *imgParams, perturb, true)
			if flp.Row > 0 && flp.Col > 0 {
				det[idx] = append(det[idx], flp.Col, flp.Row, int(flp.Scale))
			}
			idx++
		}
	}

	for _, mouth := range mouthCascade {
		for _, flpc := range flpcs[mouth] {
			flp := flpc.GetLandmarkPoint(leftEye, rightEye, *imgParams, perturb, false)
			if flp.Row > 0 && flp.Col > 0 {
				det[idx] = append(det[idx], flp.Col, flp.Row, int(flp.Scale))
			}
			idx++
		}
	}
	flp := flpcs["lp84"][0].GetLandmarkPoint(leftEye, rightEye, *imgParams, perturb, true)
	if flp.Row > 0 && flp.Col > 0 {
		det[idx] = append(det[idx], flp.Col, flp.Row, int(flp.Scale))
	}
	return det
}

// clusterDetection runs Pigo face detector core methods
// and returns a cluster with the detected faces coordinates.
func (d *Detector) clusterDetection(pixels []uint8, width, height int) []pigo.Detection {
	imgParams = &pigo.ImageParams{
		Pixels: pixels,
		Rows:   width,
		Cols:   height,
		Dim:    height,
	}
	cParams := pigo.CascadeParams{
		MinSize:     200,
		MaxSize:     480,
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		ImageParams: *imgParams,
	}

	// Run the classifier over the obtained leaf nodes and return the detection results.
	// The result contains quadruplets representing the row, column, scale and detection score.
	dets := faceClassifier.RunCascade(cParams, 0.0)

	// Calculate the intersection over union (IoU) of two clusters.
	dets = faceClassifier.ClusterDetections(dets, 0.1)

	return dets
}

// parseFlpCascades reads the facial landmark points cascades from the provided url.
func (d *Detector) parseFlpCascades(path string) (map[string][]*FlpCascade, error) {
	cascades := append(eyeCascades, mouthCascade...)
	flpcs := make(map[string][]*FlpCascade)

	pl := pigo.NewPuplocCascade()

	for _, cascade := range cascades {
		puplocCascade, err = d.ParseCascade(path + cascade)
		if err != nil {
			d.Log("Error reading the cascade file: %v", err)
		}
		flpc, err := pl.UnpackCascade(puplocCascade)
		flpcs[cascade] = append(flpcs[cascade], &FlpCascade{flpc, err})
	}
	return flpcs, err
}
