package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Config структура для конфигурации бота
type Config struct {
	BotToken      string  `json:"botToken"`
	AdminIDs      []int64 `json:"adminIDs"`
	AlbumWaitTime int     `json:"albumWaitTime"`
}

// AlbumPhoto содержит информацию о фото в альбоме
type AlbumPhoto struct {
	FileID   string `json:"fileID"`
	FilePath string `json:"filePath"`
}

// User представляет информацию о пользователе
type User struct {
	UserID           int64     `json:"userID"`
	UserFirstName    string    `json:"userFirstName"`
	UserLastName     string    `json:"userLastName,omitempty"`
	Username         string    `json:"username,omitempty"`
	RegistrationDate time.Time `json:"registrationDate"`
}

// Message представляет сообщение с тренировкой
type Message struct {
	UserID        int64        `json:"userID"`
	MessageID     int          `json:"messageID"`
	MediaGroupID  string       `json:"mediaGroupID,omitempty"`
	Text          string       `json:"text,omitempty"`
	Photos        []AlbumPhoto `json:"photos,omitempty"`
	Distance      float64      `json:"distance"`
	MessageDate   time.Time    `json:"messageDate"`
	IsAlbum       bool         `json:"isAlbum"`
	AlbumComplete bool         `json:"albumComplete"`
}

// UsersData содержит данные о пользователях
type UsersData struct {
	Users []User `json:"users"`
}

// MessagesData содержит данные о сообщениях
type MessagesData struct {
	Messages []Message `json:"messages"`
}

// AlbumTracker отслеживает альбомы фотографий
type AlbumTracker struct {
	sync.Mutex
	albums map[string]*AlbumData
}

// AlbumData содержит данные об альбоме
type AlbumData struct {
	Messages    []*tgbotapi.Message
	Timer       *time.Timer
	Complete    bool
	LastUpdated time.Time
}

// NewAlbumTracker создает новый трекер альбомов
func NewAlbumTracker() *AlbumTracker {
	return &AlbumTracker{
		albums: make(map[string]*AlbumData),
	}
}

// AddMessage добавляет сообщение в альбом
func (at *AlbumTracker) AddMessage(msg *tgbotapi.Message, waitTime time.Duration, callback func([]*tgbotapi.Message)) {
	at.Lock()
	defer at.Unlock()

	if msg.MediaGroupID == "" {
		return
	}

	data, exists := at.albums[msg.MediaGroupID]
	if !exists {
		data = &AlbumData{
			Messages:    []*tgbotapi.Message{msg},
			Complete:    false,
			LastUpdated: time.Now(),
		}
		data.Timer = time.AfterFunc(waitTime, func() {
			at.Lock()
			defer at.Unlock()
			if !data.Complete {
				data.Complete = true
				callback(data.Messages)
				delete(at.albums, msg.MediaGroupID)
			}
		})
		at.albums[msg.MediaGroupID] = data
	} else {
		data.Messages = append(data.Messages, msg)
		data.LastUpdated = time.Now()
		data.Timer.Stop()
		data.Timer = time.AfterFunc(waitTime, func() {
			at.Lock()
			defer at.Unlock()
			if !data.Complete {
				data.Complete = true
				callback(data.Messages)
				delete(at.albums, msg.MediaGroupID)
			}
		})
	}
}

// loadConfig загружает конфигурацию из файла
func loadConfig(filename string) (Config, error) {
	var config Config
	file, err := os.Open(filename)
	if err != nil {
		return config, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return config, err
	}
	return config, nil
}

// loadUsersData загружает данные пользователей
func loadUsersData(filename string) (UsersData, error) {
	var usersData UsersData
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return UsersData{Users: []User{}}, nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return usersData, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&usersData); err != nil {
		return usersData, err
	}
	return usersData, nil
}

// saveUsersData сохраняет данные пользователей
func saveUsersData(filename string, data UsersData) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// loadMessagesData загружает данные сообщений
func loadMessagesData(filename string) (MessagesData, error) {
	var messagesData MessagesData
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return MessagesData{Messages: []Message{}}, nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return messagesData, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&messagesData); err != nil {
		return messagesData, err
	}
	return messagesData, nil
}

// saveMessagesData сохраняет данные сообщений
func saveMessagesData(filename string, data MessagesData) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// savePhoto сохраняет фото на диск
func savePhoto(botToken, fileID string) (string, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", botToken, fileID))
	if err != nil {
		return "", fmt.Errorf("getFile error: %v", err)
	}
	defer resp.Body.Close()

	var fileResp struct {
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", fmt.Errorf("decode error: %v", err)
	}

	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, fileResp.Result.FilePath)
	if err := os.MkdirAll("photos", 0755); err != nil {
		return "", fmt.Errorf("create dir error: %v", err)
	}

	resp, err = http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("download error: %v", err)
	}
	defer resp.Body.Close()

	filename := fmt.Sprintf("photos/%s_%d.jpg", fileID, time.Now().Unix())
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("create file error: %v", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", fmt.Errorf("save file error: %v", err)
	}

	return filename, nil
}

// extractDistance извлекает дистанцию из текста
func extractDistance(text string) float64 {
	cleaned := strings.ReplaceAll(text, " ", "")
	if !regexp.MustCompile(`(?i)#км`).MatchString(cleaned) {
		return -1
	}

	matches := regexp.MustCompile(`\+(\d+[\.,]?\d*)`).FindAllStringSubmatch(cleaned, -1)
	if len(matches) == 0 {
		return -1
	}

	var sum float64
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		numStr := strings.Replace(match[1], ",", ".", -1)
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			continue
		}
		sum += num
	}
	return sum
}

// FloatToString преобразует float в строку
func FloatToString(f float64) string {
	str := strconv.FormatFloat(f, 'f', 10, 64)
	str = strings.TrimRight(str, "0")
	return strings.TrimRight(str, ".")
}

// isAdmin проверяет, является ли пользователь администратором
func isAdmin(userID int64, config Config) bool {
	for _, id := range config.AdminIDs {
		if userID == id {
			return true
		}
	}
	return false
}

// setupCORS настраивает заголовки CORS
func setupCORS(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// setupRESTAPI настраивает REST API
func setupRESTAPI(usersFile, messagesFile string) {
	http.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		setupCORS(&w)
		if r.Method == "OPTIONS" {
			return
		}

		data, err := loadUsersData(usersFile)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	})

	http.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
		setupCORS(&w)
		if r.Method == "OPTIONS" {
			return
		}

		data, err := loadMessagesData(messagesFile)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	})

	log.Println("REST API running on :9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}

// saveAlbumPhotos сохраняет все фото из альбома
func saveAlbumPhotos(botToken string, messages []*tgbotapi.Message) ([]AlbumPhoto, error) {
	var photos []AlbumPhoto

	for _, msg := range messages {
		if msg.Photo != nil {
			for _, photo := range msg.Photo {
				filePath, err := savePhoto(botToken, photo.FileID)
				if err != nil {
					return nil, err
				}
				photos = append(photos, AlbumPhoto{
					FileID:   photo.FileID,
					FilePath: filePath,
				})
			}
		}
	}

	return photos, nil
}

// handleAlbum обрабатывает завершение альбома
func handleAlbum(bot *tgbotapi.BotAPI, messagesFile string, messages []*tgbotapi.Message) {
	if len(messages) == 0 {
		return
	}

	mainMsg := messages[0]
	text := mainMsg.Text
	if text == "" {
		text = mainMsg.Caption
	}

	distance := extractDistance(text)
	if distance <= 0 {
		return
	}

	albumPhotos, err := saveAlbumPhotos(bot.Token, messages)
	if err != nil {
		log.Printf("Album photos save error: %v", err)
		return
	}

	messagesData, err := loadMessagesData(messagesFile)
	if err != nil {
		log.Printf("Load messages error: %v", err)
		return
	}

	messagesData.Messages = append(messagesData.Messages, Message{
		UserID:        mainMsg.From.ID,
		MessageID:     mainMsg.MessageID,
		MediaGroupID:  mainMsg.MediaGroupID,
		Text:          text,
		Photos:        albumPhotos,
		Distance:      distance,
		MessageDate:   time.Now(),
		IsAlbum:       true,
		AlbumComplete: true,
	})

	if err := saveMessagesData(messagesFile, messagesData); err != nil {
		log.Printf("Save messages error: %v", err)
		return
	}

	replyText := fmt.Sprintf(
		"Засчитано: %s км (альбом из %d фото)\nСтатистика: http://45.143.95.82:90/\nУдалить: /rm_%d",
		FloatToString(distance),
		len(albumPhotos),
		mainMsg.MessageID,
	)

	msg := tgbotapi.NewMessage(mainMsg.Chat.ID, replyText)
	if _, err := bot.Send(msg); err != nil {
		log.Printf("Send message error: %v", err)
	}
}

// handleSingleMessage обрабатывает одиночное сообщение
func handleSingleMessage(bot *tgbotapi.BotAPI, messagesFile string, msg *tgbotapi.Message) {
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	distance := extractDistance(text)
	if distance <= 0 {
		return
	}

	if msg.Photo == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Прикрепите скрин трека"))
		return
	}

	var photos []AlbumPhoto
	for _, photo := range msg.Photo {
		filePath, err := savePhoto(bot.Token, photo.FileID)
		if err != nil {
			log.Printf("Photo save error: %v", err)
			continue
		}
		photos = append(photos, AlbumPhoto{
			FileID:   photo.FileID,
			FilePath: filePath,
		})
	}

	messagesData, err := loadMessagesData(messagesFile)
	if err != nil {
		log.Printf("Load messages error: %v", err)
		return
	}

	messagesData.Messages = append(messagesData.Messages, Message{
		UserID:      msg.From.ID,
		MessageID:   msg.MessageID,
		Text:        text,
		Photos:      photos,
		Distance:    distance,
		MessageDate: time.Now(),
	})

	if err := saveMessagesData(messagesFile, messagesData); err != nil {
		log.Printf("Save messages error: %v", err)
		return
	}

	replyText := fmt.Sprintf(
		"Засчитано: %s км\nСтатистика: http://45.143.95.82:90/\nУдалить: /rm_%d",
		FloatToString(distance),
		msg.MessageID,
	)

	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, replyText))
}

// handleDelete обрабатывает команду удаления
func handleDelete(bot *tgbotapi.BotAPI, config Config, messagesFile string, msg *tgbotapi.Message) {
	userID := msg.From.ID
	isAdmin := isAdmin(userID, config)

	parts := strings.SplitN(msg.Text, "_", 2)
	if len(parts) < 2 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Формат: /rm_<ID>"))
		return
	}

	msgID, err := strconv.Atoi(parts[1])
	if err != nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Некорректный ID"))
		return
	}

	messagesData, err := loadMessagesData(messagesFile)
	if err != nil {
		log.Printf("Load messages error: %v", err)
		return
	}

	found := false
	for i, m := range messagesData.Messages {
		if m.MessageID == msgID {
			if m.UserID == userID || isAdmin {
				// Удаляем связанные фото
				for _, photo := range m.Photos {
					if err := os.Remove(photo.FilePath); err != nil {
						log.Printf("Remove photo error: %v", err)
					}
				}
				// Удаляем запись о сообщении
				messagesData.Messages = append(messagesData.Messages[:i], messagesData.Messages[i+1:]...)
				if err := saveMessagesData(messagesFile, messagesData); err != nil {
					log.Printf("Save messages error: %v", err)
				}
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Удалено: #%d\n%s", msgID, m.Text)))
			} else {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Нет прав для удаления"))
			}
			found = true
			break
		}
	}

	if !found {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Сообщение не найдено"))
	}
}

// registerUser регистрирует нового пользователя
func registerUser(usersFile string, user *tgbotapi.User) error {
	usersData, err := loadUsersData(usersFile)
	if err != nil {
		return err
	}

	for _, u := range usersData.Users {
		if u.UserID == user.ID {
			return nil // Пользователь уже существует
		}
	}

	usersData.Users = append(usersData.Users, User{
		UserID:           user.ID,
		UserFirstName:    user.FirstName,
		UserLastName:     user.LastName,
		Username:         user.UserName,
		RegistrationDate: time.Now(),
	})

	return saveUsersData(usersFile, usersData)
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Panicf("Config load error: %v", err)
	}

	bot, err := tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = false
	log.Printf("Bot started: @%s", bot.Self.UserName)

	usersFile := "JSON/users_data.json"
	messagesFile := "JSON/messages_data.json"

	go setupRESTAPI(usersFile, messagesFile)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 10
	updates := bot.GetUpdatesChan(u)

	tracker := NewAlbumTracker()
	waitTime := time.Duration(config.AlbumWaitTime) * time.Second
	if waitTime == 0 {
		waitTime = 5 * time.Second
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Игнорируем сообщения от самого бота
		if update.Message.From.ID == bot.Self.ID {
			continue
		}

		// Обработка команды удаления
		if strings.HasPrefix(update.Message.Text, "/rm") {
			handleDelete(bot, config, messagesFile, update.Message)
			continue
		}

		// Регистрация пользователя
		if err := registerUser(usersFile, update.Message.From); err != nil {
			log.Printf("User registration error: %v", err)
		}

		// Обработка альбома
		if update.Message.MediaGroupID != "" {
			tracker.AddMessage(update.Message, waitTime, func(msgs []*tgbotapi.Message) {
				handleAlbum(bot, messagesFile, msgs)
			})
			continue
		}

		// Обработка одиночного сообщения
		handleSingleMessage(bot, messagesFile, update.Message)
	}
}