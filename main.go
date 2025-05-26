package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	bottelegram "pricegoldtoday/bot"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
)

type GoldPrice struct {
	Type       string    `json:"type"`
	Dates      []string  `json:"dates"`
	BuyPrices  []float64 `json:"buy_prices"`
	SellPrices []float64 `json:"sell_prices"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var (
	rdb *redis.Client
	ctx = context.Background()
)

const (
	redisKeyPrefix  = "gold_price:"
	defaultGoldType = "doji_hn"
)

var GOLDTYPES = []string{"sjc", "doji_hn", "doji_sg", "bao_tin_minh_chau", "phu_quy_sjc", "pnj_tp_hcml", "pnj_hn"} // example gold types

func main() {
	// Initialize Redis
	initRedis()

	// Initial crawl when server starts
	if true {
		initialCrawl()
	}

	// Start cron job for crawling gold prices
	cronStopper := startCronJob()

	cronStopperTelegram := telegramCronJob()
	// Ensure cron jobs are stopped on exit
	defer func() {
		if cronStopper != nil {
			cronStopper.Stop()
			log.Println("Cron job stopped")
		}
		if cronStopperTelegram != nil {
			cronStopperTelegram.Stop()
			log.Println("Telegram cron job stopped")
		}
	}()

	// Create channel for graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server in a separate goroutine
	httpServer := startHTTPServer()

	// Wait for shutdown signal
	<-done
	log.Println("Received shutdown signal, initiating graceful shutdown...")

	// // Shutdown cron job
	// if cronStopper != nil {
	// 	cronStopper.Stop()
	// 	log.Println("Cron job stopped")
	// }

	// Shutdown HTTP server
	if httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		log.Println("HTTP server stopped")
	}

	// Close Redis connection
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			log.Printf("Redis connection close error: %v", err)
		}
		log.Println("Redis connection closed")
	}

	log.Println("Application shutdown complete")
}

func initialCrawl() {
	log.Println("Performing initial gold price crawl...")
	for _, goldType := range GOLDTYPES {
		if err := crawlAndSaveGoldPrice(goldType); err != nil {
			log.Printf("Initial crawl failed for %s: %v", goldType, err)
			continue
		}
		log.Printf("Successfully crawled initial data for %s", goldType)
	}
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // or your Redis address
		Password: "",               // no password set
		DB:       0,                // use default DB
	})

	// Test Redis connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")
}

func startCronJob() *cron.Cron {
	c := cron.New()

	// Run every 6 hours
	_, err := c.AddFunc("0 */6 * * *", func() {
		log.Println("Running scheduled gold price crawl job...")
		for _, goldType := range GOLDTYPES {
			if err := crawlAndSaveGoldPrice(goldType); err != nil {
				log.Printf("Error crawling gold price for %s: %v", goldType, err)
			} else {
				log.Printf("Successfully updated gold price for %s", goldType)
			}
		}
	})
	if err != nil {
		log.Fatalf("Error setting up cron job: %v", err)
	}

	c.Start()
	log.Println("Cron job started to run every 6 hours")

	return c
}
func telegramCronJob() *cron.Cron {
	c := cron.New()

	// Run every 7 hours
	// _, err := c.AddFunc("0 7 * * *", func() {
	_, err := c.AddFunc("@every 1m", func() {
		dataGold := &bottelegram.GoldPriceResponse{}
		for _, goldType := range GOLDTYPES {
			goldPrice, err := getGoldPriceFromRedis(goldType)
			if err != nil {
				// Nếu không có trong Redis, thử crawl mới
				if err := crawlAndSaveGoldPrice(goldType); err != nil {
					log.Printf("Failed to crawl gold price for %s: %v", goldType, err)
					continue
				}
				// Thử lấy lại từ Redis sau khi crawl
				goldPrice, err = getGoldPriceFromRedis(goldType)
				if err != nil {
					log.Printf("Still cannot get gold price for %s: %v", goldType, err)
					continue
				}
			}
			data := bottelegram.GoldPriceData{
				Type:       goldType,
				Dates:      goldPrice.Dates,
				BuyPrices:  goldPrice.BuyPrices,
				SellPrices: goldPrice.SellPrices,
				UpdatedAt:  goldPrice.UpdatedAt.Format(time.RFC3339),
			}
			if goldType == "doji_hn" {
				dataGold.DojiHN = data
			} else if goldType == "doji_sg" {
				dataGold.DojiSG = data
			} else if goldType == "pnj_tp_hcml" {
				dataGold.PNJTPHCML = data
			} else if goldType == "pnj_hn" {
				dataGold.PNJHN = data
			} else if goldType == "bao_tin_minh_chau" {
				dataGold.BaoTinMinhChau = data
			} else if goldType == "phu_quy_sjc" {
				dataGold.PhuQuySJC = data
			} else if goldType == "sjc" {
				dataGold.SJC = data
			}
		}
		log.Println("Running scheduled gold price crawl job...")
		err := bottelegram.SendGoldPriceNotification(dataGold)
		if err != nil {
			log.Printf("Error sending Telegram notification: %v", err)
		} else {
			log.Println("Successfully sent Telegram notification with gold prices")
		}
	})
	if err != nil {
		log.Fatalf("Error setting up cron job: %v", err)
	}

	c.Start()
	log.Println("Cron job started to run every 7 hours")

	return c
}

// CORS middleware
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins (for dev)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func startHTTPServer() *http.Server {
	r := mux.NewRouter()
	corsRouter := withCORS(r)

	r.HandleFunc("/api/gold-price", getGoldPriceHandler).Methods("GET")
	r.HandleFunc("/api/gold-price/{type}", getGoldPriceByTypeHandler).Methods("GET")
	r.HandleFunc("/health", healthCheckHandler).Methods("GET")

	port := "8080"
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: corsRouter,
	}

	go func() {
		log.Printf("Starting HTTP server on port %s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	return srv
}
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func getGoldPriceHandler(w http.ResponseWriter, r *http.Request) {
	// Danh sách các loại vàng cần lấy

	// Tạo map để lưu kết quả
	result := make(map[string]*GoldPrice)

	// Duyệt qua từng loại vàng
	for _, goldType := range GOLDTYPES {
		goldPrice, err := getGoldPriceFromRedis(goldType)
		if err != nil {
			// Nếu không có trong Redis, thử crawl mới
			if err := crawlAndSaveGoldPrice(goldType); err != nil {
				log.Printf("Failed to crawl gold price for %s: %v", goldType, err)
				continue
			}
			// Thử lấy lại từ Redis sau khi crawl
			goldPrice, err = getGoldPriceFromRedis(goldType)
			if err != nil {
				log.Printf("Still cannot get gold price for %s: %v", goldType, err)
				continue
			}
		}
		result[goldType] = goldPrice
	}

	// Nếu không có dữ liệu nào
	if len(result) == 0 {
		respondWithError(w, http.StatusInternalServerError, "Could not retrieve any gold prices")
		return
	}

	respondWithJSON(w, http.StatusOK, result)
}

func getGoldPriceByTypeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	goldType := vars["type"]
	getGoldPriceByType(w, r, goldType)
}

func getGoldPriceByType(w http.ResponseWriter, r *http.Request, goldType string) {
	// Try to get from Redis first
	goldPrice, err := getGoldPriceFromRedis(goldType)
	if err == nil && goldPrice != nil {
		respondWithJSON(w, http.StatusOK, goldPrice)
		return
	}

	// If not found in Redis, crawl new data
	log.Printf("Gold price for %s not found in Redis, crawling new data...", goldType)
	if err := crawlAndSaveGoldPrice(goldType); err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to crawl gold price: %v", err))
		return
	}

	// Try to get again
	goldPrice, err = getGoldPriceFromRedis(goldType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get gold price: %v", err))
		return
	}

	respondWithJSON(w, http.StatusOK, goldPrice)
}

func crawlAndSaveGoldPrice(goldType string) error {
	// Crawl data from website
	goldPrice, err := crawlGoldPrice(goldType)
	if err != nil {
		return fmt.Errorf("crawl failed: %w", err)
	}

	// Save to Redis
	if err := saveGoldPriceToRedis(goldType, goldPrice); err != nil {
		return fmt.Errorf("failed to save to Redis: %w", err)
	}

	return nil
}

func crawlGoldPrice(goldType string) (*GoldPrice, error) {
	url := fmt.Sprintf("https://24h.24hstatic.com/ajax/box_bieu_do_gia_vang/index/%s/0/0?is_template_page=1", goldType)

	// Tạo HTTP request với các headers cần thiết
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Thêm các headers theo yêu cầu của trang web
	req.Header.Add("accept", "*/*")
	req.Header.Add("accept-language", "vi-VN,vi;q=0.9,en-GB;q=0.8,en;q=0.7,ko-KR;q=0.6,ko;q=0.5,fr-FR;q=0.4,fr;q=0.3,en-US;q=0.2")
	req.Header.Add("origin", "https://www.24h.com.vn")
	req.Header.Add("priority", "u=1, i")
	req.Header.Add("referer", "https://www.24h.com.vn/")
	req.Header.Add("sec-ch-ua", `"Google Chrome";v="137", "Chromium";v="137", "Not/A)Brand";v="24"`)
	req.Header.Add("sec-ch-ua-mobile", "?0")
	req.Header.Add("sec-ch-ua-platform", `"macOS"`)
	req.Header.Add("sec-fetch-dest", "empty")
	req.Header.Add("sec-fetch-mode", "cors")
	req.Header.Add("sec-fetch-site", "cross-site")
	req.Header.Add("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")

	// Tạo HTTP client với timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Gửi request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request returned status: %d", resp.StatusCode)
	}

	// Đọc response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	fmt.Println("Crawled data successfully for gold type:", goldType, "with response length:", len(body), string(body)) // Log first 100 bytes for debugging
	chartData, err := extractChartData(string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to extract chart data: %w", err)
	}
	var buyPrices []float64
	var sellPrices []float64
	for _, series := range chartData.Series {
		if series.Name == "Mua vào" {
			buyPrices = series.Data
		} else if series.Name == "Bán ra" {
			sellPrices = series.Data
		}
		fmt.Printf("Series: %s\nData: %v\n", series.Name, series.Data)
	}
	res := &GoldPrice{
		Type:       goldType,
		Dates:      chartData.Categories,
		BuyPrices:  buyPrices,
		SellPrices: sellPrices,
		UpdatedAt:  time.Now(),
	}
	fmt.Println("Crawled gold price data:", res)
	return res, nil
}

func saveGoldPriceToRedis(goldType string, goldPrice *GoldPrice) error {
	key := redisKeyPrefix + goldType

	jsonData, err := json.Marshal(goldPrice)
	if err != nil {
		return err
	}

	return rdb.Set(ctx, key, jsonData, 0).Err()
}

func getGoldPriceFromRedis(goldType string) (*GoldPrice, error) {
	key := redisKeyPrefix + goldType

	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var goldPrice GoldPrice
	if err := json.Unmarshal([]byte(val), &goldPrice); err != nil {
		return nil, err
	}

	return &goldPrice, nil
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

type Series struct {
	Name  string
	Color string
	Data  []float64
}

type ChartData struct {
	Categories []string
	Series     []Series
}

func extractChartData(html string) (*ChartData, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var scriptContent string
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(text, "highcharts") && strings.Contains(text, "categories") {
			scriptContent = text
		}
	})

	if scriptContent == "" {
		return nil, fmt.Errorf("script chứa highcharts không được tìm thấy")
	}

	// Parse categories
	catRegex := regexp.MustCompile(`categories:\s*\[(.*?)\]`)
	catMatch := catRegex.FindStringSubmatch(scriptContent)
	if len(catMatch) < 2 {
		return nil, fmt.Errorf("không tìm thấy categories")
	}
	categoriesRaw := catMatch[1]
	categories := parseStringArray(categoriesRaw)

	// Parse series
	seriesRegex := regexp.MustCompile(`name:\s*'(.*?)',\s*color:\s*'(.*?)',\s*data:\s*\[(.*?)\]`)
	seriesMatches := seriesRegex.FindAllStringSubmatch(scriptContent, -1)

	var seriesList []Series
	for _, match := range seriesMatches {
		name := match[1]
		color := match[2]
		dataRaw := match[3]
		data := parseFloat64Array(dataRaw)
		seriesList = append(seriesList, Series{
			Name:  name,
			Color: color,
			Data:  data,
		})
	}

	return &ChartData{
		Categories: categories,
		Series:     seriesList,
	}, nil
}

func parseStringArray(input string) []string {
	rawItems := strings.Split(input, ",")
	var items []string
	for _, item := range rawItems {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, "'\"")
		items = append(items, item)
	}
	return items
}

func parseFloat64Array(input string) []float64 {
	rawItems := strings.Split(input, ",")
	var items []float64
	for _, item := range rawItems {
		var v float64
		_, err := fmt.Sscanf(strings.TrimSpace(item), "%f", &v)
		if err != nil {
			// Nếu có lỗi, có thể log và bỏ qua hoặc gán giá trị mặc định
			log.Printf("Error parsing float value '%s': %v", item, err)
			v = 0.0
		}
		items = append(items, v)
	}
	return items
}
