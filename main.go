package main

import (
    "context"
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "strings"
    "time"
    "os"
    "os/signal"

    "github.com/hashicorp/go-hclog"

    gohandlers "github.com/gorilla/handlers"
    "github.com/gorilla/mux"
)

var port = os.Getenv("PORT")
var telegramBotApiToken = os.Getenv("TELEGRAM_BOT_API_TOKEN")
var logger = hclog.Default()

// Struct that mimics the webhook response body
// https://core.telegram.org/bots/api#update
type webhookReqBody struct {
    Message struct {
        Text string `json:"text"`
        Chat struct {
            ID int64 `json:"id"`
        } `json:"chat"`
    } `json:"message"`
}

// Struct that mimics the webhook response body
// https://yugiohprices.docs.apiary.io/#reference/checking-card-prices/check-price-for-cards-print-tag/check-price-for-card's-print-tag
type ygoPricesPriceForPrintTagResponse struct {
    Status string `json:"status"`
    Data   struct {
        Name      string      `json:"name"`
        CardType  string      `json:"card_type"`
        Property  interface{} `json:"property"`
        Family    string      `json:"family"`
        Type      string      `json:"type"`
        PriceData struct {
            Name      string `json:"name"`
            PrintTag  string `json:"print_tag"`
            Rarity    string `json:"rarity"`
            PriceData struct {
                Status string `json:"status"`
                Data   struct {
                    Listings []interface{} `json:"listings"`
                    Prices   struct {
                        High      float64 `json:"high"`
                        Low       float64 `json:"low"`
                        Average   float64 `json:"average"`
                        Shift     float64 `json:"shift"`
                        Shift3    float64 `json:"shift_3"`
                        Shift7    float64 `json:"shift_7"`
                        Shift21   float64 `json:"shift_21"`
                        Shift30   float64 `json:"shift_30"`
                        Shift90   float64 `json:"shift_90"`
                        Shift180  float64 `json:"shift_180"`
                        Shift365  float64 `json:"shift_365"`
                        UpdatedAt string  `json:"updated_at"`
                    } `json:"prices"`
                } `json:"data"`
            } `json:"price_data"`
        } `json:"price_data"`
    } `json:"data"`
}

// Struct to conform to the JSON body of the send message request
// https://core.telegram.org/bots/api#sendmessage
type sendMessageReqBody struct {
    ChatID int64  `json:"chat_id"`
    Text   string `json:"text"`
}

// Send reploy to a chatID
func sendReply(chatID int64, text string) error {
    // Create the request body struct
    reqBody := &sendMessageReqBody{
        ChatID: chatID,
        Text:   text,
    }
    // Create the JSON body from the struct
    reqBytes, err := json.Marshal(reqBody)
    if err != nil {
        return err
    }

    // Send a post request with your token
    var botApiUrl = fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotApiToken)
    res, err := http.Post(botApiUrl, "application/json", bytes.NewBuffer(reqBytes))
    if err != nil {
        return err
    }
    if res.StatusCode != http.StatusOK {
        return errors.New("unexpected status" + res.Status)
    }

    return nil
}

// Fetch Card Prices from YgoPrices
func fetchCardPriceByPrintTag(printTag string) (*ygoPricesPriceForPrintTagResponse, error) {
    resp, err1 := http.Get("https://yugiohprices.com/api/price_for_print_tag/" + printTag)
    if err1 != nil {
        logger.Error("Ygoprices price for print tag request error", "error", err1)

        return nil, err1
    }
    defer resp.Body.Close()

    // First, decode the JSON response body
    ygoPricesPriceForPrintTagResponse := &ygoPricesPriceForPrintTagResponse{}
    if err := json.NewDecoder(resp.Body).Decode(ygoPricesPriceForPrintTagResponse); err != nil {
        logger.Error("Parse ygoprices price for print tag response", "error", err)

        return nil, err
    }

    if ygoPricesPriceForPrintTagResponse.Status == "success" {
        return ygoPricesPriceForPrintTagResponse, nil
    }

    return nil, nil
}

// Convert YgoPrices Price For Print Tag Response to Telegram Reply
func convertYgoPricesPriceForPrintTagResponseToReply(body *ygoPricesPriceForPrintTagResponse) string {
    high := body.Data.PriceData.PriceData.Data.Prices.High
    average := body.Data.PriceData.PriceData.Data.Prices.Average
    low := body.Data.PriceData.PriceData.Data.Prices.Low

    return fmt.Sprintf("Prices\nHigh :$%.2f\nAverage: $%.2f\nLow: $%.2f", high, average, low)
}

// This handler is called everytime telegram sends us a webhook event
func webhookHandler(res http.ResponseWriter, req *http.Request) {
    // First, decode the JSON response body
    body := &webhookReqBody{}
    if err := json.NewDecoder(req.Body).Decode(body); err != nil {
        logger.Error("Parse webhook request", "error", err)
        return
    }

    var text = strings.ToLower(body.Message.Text)
    if strings.Contains(text, "/priceprinttag") {
        parts := strings.Split(body.Message.Text, " ")
        if len(parts) < 2 {
            sendReply(body.Message.Chat.ID, "Error fetching card price!")
        } else {
            printTag := parts[1]
            response, err := fetchCardPriceByPrintTag(printTag)
            if err != nil || response == nil {
                sendReply(body.Message.Chat.ID, "Error fetching card price!")
            } else {
                reply := convertYgoPricesPriceForPrintTagResponseToReply(response)
                sendReply(body.Message.Chat.ID, reply)
            }
        }
    } else {
        sendReply(body.Message.Chat.ID, "Invalid command!")
    }

    // log a confirmation message if the message is sent successfully
    logger.Info("reply sent", body.Message.Chat.ID)
}

func main() {
    // create a new serve mux and register the handlers
    sm := mux.NewRouter()

    // handlers for API
    getR := sm.Methods(http.MethodPost).Subrouter()
    getR.HandleFunc("/", webhookHandler)

    // CORS
    ch := gohandlers.CORS(gohandlers.AllowedOrigins([]string{"*"}))

    // create a new server
    s := http.Server{
        Addr:         ":" + port,                                     // configure the bind address
        Handler:      ch(sm),                                           // set the default handler
        ErrorLog:     logger.StandardLogger(&hclog.StandardLoggerOptions{}), // set the logger for the server
        ReadTimeout:  5 * time.Second,                                  // max time to read request from the client
        WriteTimeout: 10 * time.Second,                                 // max time to write response to the client
        IdleTimeout:  120 * time.Second,                                // max time for connections using TCP Keep-Alive
    }

    // start the server
    go func() {
        logger.Info("Starting server...")

        err := s.ListenAndServe()
        if err != nil {
            logger.Error("Error starting server", "error", err)
            os.Exit(1)
        }
    }()

    // trap sigterm or interupt and gracefully shutdown the server
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt)
    signal.Notify(c, os.Kill)

    // Block until a signal is received.
    sig := <-c
    logger.Info("Got signal:", sig)

    // gracefully shutdown the server, waiting max 30 seconds for current operations to complete
    ctx, _ := context.WithTimeout(context.Background(), 30*time.Second)
    s.Shutdown(ctx)
}
