package bottelegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strings"
	"time"
)

// GoldPriceData represents the structure of gold price data
type GoldPriceData struct {
	Type       string    `json:"type"`
	Dates      []string  `json:"dates"`
	BuyPrices  []float64 `json:"buy_prices"`
	SellPrices []float64 `json:"sell_prices"`
	UpdatedAt  string    `json:"updated_at"`
}

// GoldPriceResponse represents the complete response structure
type GoldPriceResponse struct {
	BaoTinMinhChau GoldPriceData `json:"bao_tin_minh_chau"`
	DojiHN         GoldPriceData `json:"doji_hn"`
	DojiSG         GoldPriceData `json:"doji_sg"`
	PhuQuySJC      GoldPriceData `json:"phu_quy_sjc"`
	PNJHN          GoldPriceData `json:"pnj_hn"`
	PNJTPHCML      GoldPriceData `json:"pnj_tp_hcml"`
	SJC            GoldPriceData `json:"sjc"`
}

// Config holds the Telegram bot configuration
type Config struct {
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
	DataURL          string `json:"data_url"`
}

func loadConfig(filename string) (*Config, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func SendGoldPriceNotification(goldData *GoldPriceResponse) error {
	// Load configuration
	config, err := loadConfig("bot/config.json")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
	}
	// Format the message
	message := formatGoldPriceMessage(goldData)

	// Send to Telegram
	err = sendTelegramMessage(config.TelegramBotToken, config.TelegramChatID, message)
	if err != nil {
		fmt.Printf("Error sending Telegram message: %v\n", err)
		return err
	}

	fmt.Println("Gold price notification sent successfully!")
	return nil
}
func formatGoldPriceMessage(data *GoldPriceResponse) string {
	now := time.Now()
	today := now.Format("02/01")
	yesterday := now.AddDate(0, 0, -1).Format("02/01")
	updateTime := now.Format("15:04 02/01/2006")

	// Format helpers
	formatMillions := func(price float64) string {
		return fmt.Sprintf("%.1f", price/1e6)
	}

	getChangeIcon := func(current, prev float64) string {
		if prev == 0 {
			return "↔ 0.0 (0.0%)"
		}

		diff := (current - prev) / 1e6
		percent := (current - prev) / prev * 100
		absDiff, absPercent := math.Abs(diff), math.Abs(percent)

		switch {
		case diff > 0:
			return fmt.Sprintf("↑%.1f (%.1f%%)", absDiff, absPercent)
		case diff < 0:
			return fmt.Sprintf("↓%.1f (%.1f%%)", absDiff, absPercent)
		default:
			return "↔0.0 (0.0%)"
		}
	}

	// Provider data
	providers := []struct {
		name string
		data GoldPriceData
	}{
		{"Bảo Tín Minh Châu", data.BaoTinMinhChau},
		{"DOJI HN", data.DojiHN},
		{"DOJI SG", data.DojiSG},
		{"Phú Quý SJC", data.PhuQuySJC},
		{"PNJ HN", data.PNJHN},
		{"PNJ TP.HCM", data.PNJTPHCML},
		{"SJC", data.SJC},
	}

	// Build table
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("💰 <b>BẢNG GIÁ VÀNG NGÀY %s</b> 💰\n", today))
	sb.WriteString("<pre>\n")
	sb.WriteString("| CỬA HÀNG        | MUA VÀO (THAY ĐỔI) | BÁN RA (THAY ĐỔI) |\n")
	sb.WriteString("|-----------------|--------------------|--------------------|\n")

	for _, p := range providers {
		var todayBuy, todaySell, yesterdayBuy, yesterdaySell float64

		for i, date := range p.data.Dates {
			switch date {
			case today:
				todayBuy, todaySell = p.data.BuyPrices[i], p.data.SellPrices[i]
			case yesterday:
				yesterdayBuy, yesterdaySell = p.data.BuyPrices[i], p.data.SellPrices[i]
			}
		}

		sb.WriteString(fmt.Sprintf(
			"| %-15s | %6s (%s) | %6s (%s) |\n",
			p.name,
			formatMillions(todayBuy),
			getChangeIcon(todayBuy, yesterdayBuy),
			formatMillions(todaySell),
			getChangeIcon(todaySell, yesterdaySell),
		))
	}

	sb.WriteString("</pre>\n")
	sb.WriteString(fmt.Sprintf("📊 So sánh với ngày %s\n", yesterday))
	sb.WriteString(fmt.Sprintf("⏰ Cập nhật: %s", updateTime))

	return sb.String()
}

// func formatGoldPriceMessage(data *GoldPriceResponse) string {
// 	today := time.Now().Format("02/01")
// 	yesterday := time.Now().AddDate(0, 0, -1).Format("02/01")

// 	// Helper functions
// 	formatPrice := func(price float64) string {
// 		return fmt.Sprintf("%.1f", float64(price)/1000000)
// 	}

// 	calculateChange := func(current, prev float64) string {
// 		if prev == 0 {
// 			return "↔ 0.0 (0.0%)"
// 		}
// 		diff := float64(current-prev) / 1000000
// 		percent := (float64(current-prev) / float64(prev)) * 100

// 		var icon string
// 		switch {
// 		case diff > 0:
// 			icon = fmt.Sprintf("↑%.1f (%.1f%%)", diff, percent)
// 		case diff < 0:
// 			icon = fmt.Sprintf("↓%.1f (%.1f%%)", -diff, -percent)
// 		default:
// 			icon = fmt.Sprintf("↔0.0 (0.0%%)")
// 		}
// 		return icon
// 	}

// 	// Build table rows
// 	var rows []string
// 	providers := []struct {
// 		name string
// 		data GoldPriceData
// 	}{
// 		{"Bảo Tín Minh Châu", data.BaoTinMinhChau},
// 		{"DOJI HN", data.DojiHN},
// 		{"DOJI SG", data.DojiSG},
// 		{"Phú Quý SJC", data.PhuQuySJC},
// 		{"PNJ HN", data.PNJHN},
// 		{"PNJ TP.HCM", data.PNJTPHCML},
// 		{"SJC", data.SJC},
// 	}

// 	for _, provider := range providers {
// 		var todayBuy, todaySell, yesterdayBuy, yesterdaySell float64

// 		// Find prices
// 		for i, date := range provider.data.Dates {
// 			if date == today {
// 				todayBuy = provider.data.BuyPrices[i]
// 				todaySell = provider.data.SellPrices[i]
// 			}
// 			if date == yesterday {
// 				yesterdayBuy = provider.data.BuyPrices[i]
// 				yesterdaySell = provider.data.SellPrices[i]
// 			}
// 		}

// 		// Calculate changes for both buy and sell prices
// 		buyChange := calculateChange(todayBuy, yesterdayBuy)
// 		sellChange := calculateChange(todaySell, yesterdaySell)

// 		rows = append(rows, fmt.Sprintf(
// 			"| %-15s | %6s (%s) | %6s (%s) |",
// 			provider.name,
// 			formatPrice(todayBuy),
// 			buyChange,
// 			formatPrice(todaySell),
// 			sellChange,
// 		))
// 	}

// 	// Compose final message
// 	message := "💰 <b>BẢNG GIÁ VÀNG NGÀY " + today + "</b> 💰\n"
// 	message += "<pre>\n"
// 	message += "| CỬA HÀNG        | MUA VÀO (THAY ĐỔI) | BÁN RA (THAY ĐỔI) |\n"
// 	message += "|-----------------|--------------------|--------------------|\n"
// 	message += strings.Join(rows, "\n") + "\n"
// 	message += "</pre>\n"
// 	message += "📊 So sánh với ngày " + yesterday + "\n"
// 	message += "⏰ Cập nhật: " + time.Now().Format("15:04 02/01/2006")

// 	return message
// }

func sendTelegramMessage(botToken, chatID, message string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]string{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	return nil
}
