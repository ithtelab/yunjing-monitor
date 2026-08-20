package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"strings"
	"sync"
	"time"
)

type captchaEntry struct {
	Code      string
	ExpiresAt time.Time
}

// CaptchaStore is an in-memory one-time captcha pool.
type CaptchaStore struct {
	mu      sync.Mutex
	entries map[string]captchaEntry
}

const maxCaptchaEntries = 4096

func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{entries: map[string]captchaEntry{}}
}

func (c *CaptchaStore) Issue() (id, imageBase64 string, err error) {
	c.cleanup()
	c.mu.Lock()
	full := len(c.entries) >= maxCaptchaEntries
	c.mu.Unlock()
	if full {
		return "", "", fmt.Errorf("captcha capacity reached")
	}
	answer, err := randomCaptchaCode(5)
	if err != nil {
		return "", "", err
	}
	idBuf := make([]byte, 16)
	if _, err := rand.Read(idBuf); err != nil {
		return "", "", err
	}
	id = base64.RawURLEncoding.EncodeToString(idBuf)
	img, err := renderCaptchaPNG(answer)
	if err != nil {
		return "", "", err
	}
	c.mu.Lock()
	if len(c.entries) >= maxCaptchaEntries {
		c.mu.Unlock()
		return "", "", fmt.Errorf("captcha capacity reached")
	}
	c.entries[id] = captchaEntry{Code: answer, ExpiresAt: time.Now().Add(5 * time.Minute)}
	c.mu.Unlock()
	return id, "data:image/png;base64," + base64.StdEncoding.EncodeToString(img), nil
}

// Verify consumes the captcha (one-time). Case-insensitive.
func (c *CaptchaStore) Verify(id, code string) bool {
	id = strings.TrimSpace(id)
	code = strings.TrimSpace(code)
	if id == "" || code == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok {
		return false
	}
	delete(c.entries, id)
	if time.Now().After(entry.ExpiresAt) {
		return false
	}
	return strings.EqualFold(entry.Code, code)
}

func (c *CaptchaStore) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for id, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, id)
		}
	}
}

func randomCaptchaCode(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}

func renderCaptchaPNG(code string) ([]byte, error) {
	const (
		charW = 6
		charH = 8
		padX  = 8
		padY  = 6
		scale = 3
	)
	width := padX*2 + len(code)*charW*scale
	height := padY*2 + charH*scale
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: 245, G: 247, B: 250, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	for i := 0; i < 4; i++ {
		x0, _ := rand.Int(rand.Reader, big.NewInt(int64(width)))
		y0, _ := rand.Int(rand.Reader, big.NewInt(int64(height)))
		x1, _ := rand.Int(rand.Reader, big.NewInt(int64(width)))
		y1, _ := rand.Int(rand.Reader, big.NewInt(int64(height)))
		drawCaptchaLine(img, int(x0.Int64()), int(y0.Int64()), int(x1.Int64()), int(y1.Int64()), color.RGBA{R: 180, G: 190, B: 200, A: 255})
	}

	fg := color.RGBA{R: 30, G: 40, B: 60, A: 255}
	for i, ch := range code {
		glyph, ok := captchaGlyphs[ch]
		if !ok {
			continue
		}
		ox := padX + i*charW*scale
		oy := padY
		for gy := 0; gy < charH; gy++ {
			row := glyph[gy]
			for gx := 0; gx < charW; gx++ {
				if row&(1<<(charW-1-gx)) == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						img.Set(ox+gx*scale+sx, oy+gy*scale+sy, fg)
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		return nil, fmt.Errorf("empty captcha image")
	}
	return buf.Bytes(), nil
}

func drawCaptchaLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= img.Bounds().Min.X && x0 < img.Bounds().Max.X && y0 >= img.Bounds().Min.Y && y0 < img.Bounds().Max.Y {
			img.Set(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// 6x8 bitmaps; bit 5 is left-most pixel of each row.
var captchaGlyphs = map[rune][8]byte{
	'A': {0x0C, 0x1E, 0x33, 0x33, 0x3F, 0x33, 0x33, 0x00},
	'B': {0x3E, 0x33, 0x33, 0x3E, 0x33, 0x33, 0x3E, 0x00},
	'C': {0x1E, 0x33, 0x30, 0x30, 0x30, 0x33, 0x1E, 0x00},
	'D': {0x3C, 0x36, 0x33, 0x33, 0x33, 0x36, 0x3C, 0x00},
	'E': {0x3F, 0x30, 0x30, 0x3E, 0x30, 0x30, 0x3F, 0x00},
	'F': {0x3F, 0x30, 0x30, 0x3E, 0x30, 0x30, 0x30, 0x00},
	'G': {0x1E, 0x33, 0x30, 0x37, 0x33, 0x33, 0x1E, 0x00},
	'H': {0x33, 0x33, 0x33, 0x3F, 0x33, 0x33, 0x33, 0x00},
	'J': {0x07, 0x03, 0x03, 0x03, 0x33, 0x33, 0x1E, 0x00},
	'K': {0x33, 0x36, 0x3C, 0x38, 0x3C, 0x36, 0x33, 0x00},
	'L': {0x30, 0x30, 0x30, 0x30, 0x30, 0x30, 0x3F, 0x00},
	'M': {0x33, 0x3F, 0x3F, 0x33, 0x33, 0x33, 0x33, 0x00},
	'N': {0x33, 0x3B, 0x3F, 0x37, 0x33, 0x33, 0x33, 0x00},
	'P': {0x3E, 0x33, 0x33, 0x3E, 0x30, 0x30, 0x30, 0x00},
	'Q': {0x1E, 0x33, 0x33, 0x33, 0x37, 0x36, 0x1D, 0x00},
	'R': {0x3E, 0x33, 0x33, 0x3E, 0x3C, 0x36, 0x33, 0x00},
	'S': {0x1E, 0x33, 0x30, 0x1E, 0x03, 0x33, 0x1E, 0x00},
	'T': {0x3F, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x00},
	'U': {0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x1E, 0x00},
	'V': {0x33, 0x33, 0x33, 0x33, 0x33, 0x1E, 0x0C, 0x00},
	'W': {0x33, 0x33, 0x33, 0x33, 0x3F, 0x3F, 0x33, 0x00},
	'X': {0x33, 0x33, 0x1E, 0x0C, 0x1E, 0x33, 0x33, 0x00},
	'Y': {0x33, 0x33, 0x1E, 0x0C, 0x0C, 0x0C, 0x0C, 0x00},
	'Z': {0x3F, 0x03, 0x06, 0x0C, 0x18, 0x30, 0x3F, 0x00},
	'2': {0x1E, 0x33, 0x03, 0x0E, 0x18, 0x30, 0x3F, 0x00},
	'3': {0x1E, 0x33, 0x03, 0x0E, 0x03, 0x33, 0x1E, 0x00},
	'4': {0x06, 0x0E, 0x1E, 0x36, 0x3F, 0x06, 0x06, 0x00},
	'5': {0x3F, 0x30, 0x3E, 0x03, 0x03, 0x33, 0x1E, 0x00},
	'6': {0x0E, 0x18, 0x30, 0x3E, 0x33, 0x33, 0x1E, 0x00},
	'7': {0x3F, 0x03, 0x06, 0x0C, 0x18, 0x18, 0x18, 0x00},
	'8': {0x1E, 0x33, 0x33, 0x1E, 0x33, 0x33, 0x1E, 0x00},
	'9': {0x1E, 0x33, 0x33, 0x1F, 0x03, 0x06, 0x1C, 0x00},
}
