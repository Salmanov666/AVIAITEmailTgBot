package main

import (
	"encoding/json"
	"flag" // Импортируем пакет для работы с аргументами командной строки
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	SECRETS_FILE = "secrets.json"
	// Define the text for the "New Letter" button
	NEW_LETTER_BUTTON_TEXT = "Новое Письмо"
)

// Secrets holds the API keys, tokens, and other configuration details.
type Secrets struct {
	BotToken        string `json:"bot_token"`
	UnisenderAPIKey string `json:"unisender_api_key"`
	TargetEmail     string `json:"target_email"` // Target email address
	SenderEmail     string `json:"sender_email"` // Verified sender email in Unisender
	LogFile         string `json:"log_file"`     // File for logging errors
}

// UserState holds the current state of interaction for a user.
type UserState struct {
	State      string // Current step in the email sending process
	Subject    string // Email subject
	Body       string // Email body
	SenderName string // Sender's name
}

// states maps user IDs to their current UserState.
var states = make(map[int64]*UserState)

// UnisenderResponse represents the expected structure of the Unisender API response.
type UnisenderResponse struct {
	Result json.RawMessage `json:"result"`          // Can be an array of IDs or an object with error details
	Error  string          `json:"error,omitempty"` // Top-level error string
}

// loadSecrets reads configuration details from a JSON file.
func loadSecrets(filename string) (*Secrets, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		// If secrets file is not found, it's not necessarily an error if using command line args
		log.Printf("Файл секретов %s не найден или ошибка чтения: %v. Используются аргументы командной строки.", filename, err)
		return &Secrets{}, nil // Return empty secrets struct, validation will happen later
	}

	var secrets Secrets
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("ошибка разбора файла %s: %w", filename, err)
	}

	return &secrets, nil
}

// setupLogging configures logging to write to a file, overwriting it on each run.
func setupLogging(filename string) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Ошибка открытия файла логов %s: %v", filename, err)
	}
	log.SetOutput(file)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile) // Add date, time, and file/line number to logs
}

// SendEmailViaUnisender sends an email using the Unisender API.
// It now accepts targetEmail and senderEmail as parameters.
func SendEmailViaUnisender(apiKey, targetEmail, senderEmail, subject, body, senderName string) (*UnisenderResponse, error) {
	apiURL := "https://api.unisender.com/ru/api/sendEmail"

	data := url.Values{
		"format":         {"json"},
		"api_key":        {apiKey},
		"sender_name":    {senderName},
		"sender_email":   {senderEmail},
		"email":          {targetEmail},
		"subject":        {subject},
		"body":           {body},
		"list_id":        {"1"},
		"error_checking": {"1"},
	}

	log.Printf("Подготовка отправки письма: Тема: %s, Имя: %s, Получатель: %s", subject, senderName, targetEmail)

	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		log.Printf("Ошибка запроса к Unisender: %v", err)
		return nil, fmt.Errorf("ошибка HTTP запроса: %w", err)
	}
	defer resp.Body.Close()

	var result UnisenderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Ошибка декодирования ответа Unisender: %v", err)
		// Log the raw response body if decoding fails to help diagnose unexpected formats
		// Note: Reading resp.Body again requires seeking or reading into a buffer first.
		// For simplicity here, we just report the decoding error.
		return nil, fmt.Errorf("ошибка декодирования ответа: %w", err)
	}

	log.Printf("Ответ от Unisender: %+v", result)
	log.Printf("Raw result: %s", string(result.Result)) // Log the raw JSON message

	return &result, nil
}

func main() {
	// Define command-line flags
	botTokenArg := flag.String("bot-token", "", "Токен Telegram бота")
	unisenderAPIKeyArg := flag.String("unisender-api-key", "", "API ключ Unisender")
	targetEmailArg := flag.String("target-email", "", "Email получателя")
	senderEmailArg := flag.String("sender-email", "", "Email отправителя")
	logFileArg := flag.String("log-file", "bot_errors.log", "Файл для логов")

	// Parse command-line arguments
	flag.Parse()

	// Load secrets from file
	fileSecrets, err := loadSecrets(SECRETS_FILE)
	if err != nil {
		// loadSecrets already logs the error, just exit if file reading/parsing failed
		os.Exit(1)
	}

	// Use command-line arguments if provided, otherwise use secrets from file
	secrets := Secrets{
		BotToken:        choose(*botTokenArg, fileSecrets.BotToken),
		UnisenderAPIKey: choose(*unisenderAPIKeyArg, fileSecrets.UnisenderAPIKey),
		TargetEmail:     choose(*targetEmailArg, fileSecrets.TargetEmail),
		SenderEmail:     choose(*senderEmailArg, fileSecrets.SenderEmail),
		LogFile:         choose(*logFileArg, fileSecrets.LogFile),
	}

	// Validate that required secrets are available
	if secrets.BotToken == "" {
		log.Fatalf("Не указан токен Telegram бота. Используйте аргумент --bot-token или файл secrets.json.")
	}
	if secrets.UnisenderAPIKey == "" {
		log.Fatalf("Не указан API ключ Unisender. Используйте аргумент --unisender-api-key или файл secrets.json.")
	}
	if secrets.TargetEmail == "" {
		log.Fatalf("Не указан email получателя. Используйте аргумент --target-email или файл secrets.json.")
	}
	if secrets.SenderEmail == "" {
		log.Fatalf("Не указан email отправителя. Используйте аргумент --sender-email или файл secrets.json.")
	}
	// LogFile has a default value in the flag definition, but if fileSecrets had one, use it
	if *logFileArg == "bot_errors.log" && fileSecrets.LogFile != "" {
		secrets.LogFile = fileSecrets.LogFile
	}

	// Setup logging to a file using the filename from secrets
	setupLogging(secrets.LogFile)
	log.Println("Бот запущен") // Log bot start

	bot, err := tgbotapi.NewBotAPI(secrets.BotToken)
	if err != nil {
		log.Fatalf("Ошибка инициализации Telegram бота: %v", err)
	}

	bot.Debug = true // Enable debug logging for Telegram updates
	log.Printf("Авторизация в аккаунте Telegram: %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60 // Long polling timeout

	updates := bot.GetUpdatesChan(u)

	// Define the initial keyboard with the "New Letter" button
	initialKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(NEW_LETTER_BUTTON_TEXT),
		),
	)
	initialKeyboard.OneTimeKeyboard = false // Keep the keyboard visible

	for update := range updates {
		if update.Message == nil { // Ignore non-message updates
			continue
		}

		userID := update.Message.From.ID
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)

		log.Printf("[%s] Получено сообщение: %s (ID пользователя: %d)", update.Message.From.UserName, text, userID)

		// Handle the /start command to show the initial keyboard
		if text == "/start" {
			// Reset state for the user and show the initial keyboard
			states[userID] = &UserState{State: "initial"} // Set state to initial
			msg := tgbotapi.NewMessage(chatID, "Привет! Нажмите кнопку 'Новое Письмо', чтобы начать отправку.")
			msg.ReplyMarkup = initialKeyboard // Show the initial keyboard
			bot.Send(msg)
			continue // Process next update
		}

		// Retrieve user state, prompt /start if not found or if state is initial and text is not the button
		state, exists := states[userID]
		if !exists || (state.State == "initial" && text != NEW_LETTER_BUTTON_TEXT) {
			// If state doesn't exist, or if in initial state and received unexpected text
			if !exists {
				states[userID] = &UserState{State: "initial"}
			}
			msg := tgbotapi.NewMessage(chatID, "Пожалуйста, начните с команды /start или нажмите 'Новое Письмо'.")
			msg.ReplyMarkup = initialKeyboard // Show the initial keyboard
			bot.Send(msg)
			continue // Process next update
		}

		// State machine to guide the user through the email sending process
		switch state.State {
		case "initial":
			// This case is now only reached if text == NEW_LETTER_BUTTON_TEXT because of the check above
			state.State = "await_subject" // Transition to awaiting subject
			msg := tgbotapi.NewMessage(chatID, "Введите тему письма.")
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(false) // Remove the custom keyboard
			bot.Send(msg)

		case "await_subject":
			state.Subject = text
			state.State = "await_body"
			bot.Send(tgbotapi.NewMessage(chatID, "Введите текст письма."))

		case "await_body":
			state.Body = text
			state.State = "await_sender"
			bot.Send(tgbotapi.NewMessage(chatID, "Укажите имя отправителя."))

		case "await_sender":
			state.SenderName = text
			bot.Send(tgbotapi.NewMessage(chatID, "Отправляю письмо..."))

			var finalMsgText string
			result, err := SendEmailViaUnisender(secrets.UnisenderAPIKey, secrets.TargetEmail, secrets.SenderEmail, state.Subject, state.Body, state.SenderName)

			if err != nil {
				// Handle errors during the HTTP request or response decoding
				finalMsgText = fmt.Sprintf("Ошибка при отправке письма: %v", err)
				log.Printf("Ошибка отправки письма: %v", err)
			} else if result.Error != "" {
				// Handle API-level errors indicated by the 'error' field
				finalMsgText = fmt.Sprintf("Ошибка API Unisender: %s", result.Error)
				log.Printf("Ошибка API Unisender: %s", result.Error)
			} else {
				// No top-level error from Unisender, assume success and try to get the ID
				var emailIDs []int64
				unmarshalErr := json.Unmarshal(result.Result, &emailIDs)

				if unmarshalErr == nil && len(emailIDs) > 0 {
					// Successfully unmarshalled and found email IDs
					finalMsgText = fmt.Sprintf("Письмо успешно отправлено, ID: %d", emailIDs[0])
					log.Printf("Письмо успешно отправлено, ID: %d", emailIDs[0])
				} else {
					// Unmarshalling failed or emailIDs slice is empty, BUT Unisender reported no error.
					// This means the email was likely sent, but the result format was unexpected.
					log.Printf("Неожиданный формат ответа: %v, Raw result: %s", unmarshalErr, string(result.Result))
					finalMsgText = "Письмо успешно отправлено!" // Generic success message
				}
			}

			// Always set state back to initial after sending attempt
			state.State = "initial"

			// Send the final message with the initial keyboard attached
			msg := tgbotapi.NewMessage(chatID, finalMsgText+"\nХотите отправить ещё одно письмо? Нажмите 'Новое Письмо'.")
			msg.ReplyMarkup = initialKeyboard
			bot.Send(msg)
		}
	}
}

// choose returns the first string if it's not empty, otherwise returns the fallback.
func choose(arg, fallback string) string {
	if arg != "" {
		return arg
	}
	return fallback
}
