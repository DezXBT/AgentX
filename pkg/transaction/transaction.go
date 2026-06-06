package transaction

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Generator is a faithful port of XClientTransaction.ClientTransaction. It is
// safe for concurrent use: GenerateTransactionID only reads immutable state and
// uses package-level randomness.
type Generator struct {
	doc              *goquery.Document
	key              string
	keyBytes         []int
	animationKey     string
	rowIndex         int
	keyBytesIndices  []int
	randomKeyword    string
	additionalNumber int
}

// Option customises a Generator.
type Option func(*Generator)

// WithKeyword overrides the obfuscation keyword (defaults to "obfiowerehiring").
func WithKeyword(k string) Option { return func(g *Generator) { g.randomKeyword = k } }

// WithNumber overrides the static appended byte (defaults to 3).
func WithNumber(n int) Option { return func(g *Generator) { g.additionalNumber = n } }

var dotDashRegex = regexp.MustCompile(`[.-]`)

// New builds a Generator from the x.com home page HTML and the ondemand.s file
// text. Both are fetched without authentication (see Provider for a helper that
// retrieves and caches them).
func New(homeHTML, ondemandText string, opts ...Option) (*Generator, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(homeHTML))
	if err != nil {
		return nil, fmt.Errorf("transaction: parse home page: %w", err)
	}

	g := &Generator{
		doc:              doc,
		randomKeyword:    defaultKeyword,
		additionalNumber: additionalRandomNumber,
	}
	for _, o := range opts {
		o(g)
	}

	if g.rowIndex, g.keyBytesIndices, err = getIndices(ondemandText); err != nil {
		return nil, err
	}
	if g.key, err = getKey(doc); err != nil {
		return nil, err
	}
	keyBytes, err := decodeKeyBytes(g.key)
	if err != nil {
		return nil, err
	}
	g.keyBytes = keyBytes
	if g.animationKey, err = g.getAnimationKey(); err != nil {
		return nil, err
	}
	return g, nil
}

func decodeKeyBytes(key string) ([]int, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("transaction: decode verification key: %w", err)
	}
	out := make([]int, len(raw))
	for i, b := range raw {
		out[i] = int(b)
	}
	return out, nil
}

func (g *Generator) getAnimationKey() (string, error) {
	rowIndex := g.keyBytes[g.rowIndex] % 16

	frameTime := 1
	for _, idx := range g.keyBytesIndices {
		frameTime *= g.keyBytes[idx] % 16
	}
	frameTimeF := mathRound(float64(frameTime)/10) * 10

	arr, err := get2DArray(g.doc, g.keyBytes)
	if err != nil {
		return "", err
	}
	if rowIndex >= len(arr) {
		return "", fmt.Errorf("transaction: frame row index %d out of range (%d rows)", rowIndex, len(arr))
	}
	frameRow := arr[rowIndex]
	if len(frameRow) < 11 {
		return "", fmt.Errorf("transaction: frame row too short (%d values)", len(frameRow))
	}

	targetTime := frameTimeF / totalTime
	return animate(frameRow, targetTime), nil
}

// animate ports ClientTransaction.animate.
func animate(frames []int, targetTime float64) string {
	fromColor := []float64{float64(frames[0]), float64(frames[1]), float64(frames[2]), 1}
	toColor := []float64{float64(frames[3]), float64(frames[4]), float64(frames[5]), 1}
	fromRotation := []float64{0.0}
	toRotation := []float64{solve(float64(frames[6]), 60.0, 360.0, true)}

	rest := frames[7:]
	curves := make([]float64, len(rest))
	for i, item := range rest {
		curves[i] = solve(float64(item), isOdd(i), 1.0, false)
	}

	val := newCubic(curves).getValue(targetTime)

	color := interpolate(fromColor, toColor, val)
	for i := range color {
		color[i] = math.Max(0, math.Min(255, color[i]))
	}
	rotation := interpolate(fromRotation, toRotation, val)
	matrix := convertRotationToMatrix(rotation[0])

	var sb strings.Builder
	for _, value := range color[:len(color)-1] {
		sb.WriteString(strconv.FormatInt(int64(roundHalfEven(value)), 16))
	}
	for _, value := range matrix {
		rounded := roundTo(value, 2)
		if rounded < 0 {
			rounded = -rounded
		}
		hexValue := floatToHex(rounded)
		switch {
		case strings.HasPrefix(hexValue, "."):
			sb.WriteString(strings.ToLower("0" + hexValue))
		case hexValue != "":
			sb.WriteString(hexValue)
		default:
			sb.WriteString("0")
		}
	}
	sb.WriteString("00")

	return dotDashRegex.ReplaceAllString(sb.String(), "")
}

// GenerateTransactionID produces the x-client-transaction-id value for an HTTP
// method and request path (e.g. "/i/api/graphql/<qid>/UserByScreenName").
func (g *Generator) GenerateTransactionID(method, path string) string {
	timeNow := int64(math.Floor(float64(time.Now().UnixMilli()-epochOffsetMillis) / 1000))
	timeNowBytes := []int{
		int(timeNow & 0xFF),
		int((timeNow >> 8) & 0xFF),
		int((timeNow >> 16) & 0xFF),
		int((timeNow >> 24) & 0xFF),
	}

	input := fmt.Sprintf("%s!%s!%d%s%s", method, path, timeNow, g.randomKeyword, g.animationKey)
	sum := sha256.Sum256([]byte(input))

	bytesArr := make([]int, 0, len(g.keyBytes)+4+16+1)
	bytesArr = append(bytesArr, g.keyBytes...)
	bytesArr = append(bytesArr, timeNowBytes...)
	for _, b := range sum[:16] {
		bytesArr = append(bytesArr, int(b))
	}
	bytesArr = append(bytesArr, g.additionalNumber)

	randomNum := rand.Intn(256)
	out := make([]byte, 0, len(bytesArr)+1)
	out = append(out, byte(randomNum))
	for _, item := range bytesArr {
		out = append(out, byte(item^randomNum))
	}

	return strings.TrimRight(base64.StdEncoding.EncodeToString(out), "=")
}

// GenerateForURL is a convenience wrapper that extracts the path from a full URL.
func (g *Generator) GenerateForURL(method, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("transaction: parse url: %w", err)
	}
	return g.GenerateTransactionID(method, u.Path), nil
}

// Key returns the verification key extracted from the home page.
func (g *Generator) Key() string { return g.key }

// AnimationKey returns the computed animation key (useful for debugging/tests).
func (g *Generator) AnimationKey() string { return g.animationKey }
