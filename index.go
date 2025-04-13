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
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Конфигурация бота
type Config struct {
	BotToken      string  `json:"botToken"`
	AdminIDs      []int64 `json:"adminIDs"`
}

// Структуры для парсинга JSON от Telegram API
type UserProfilePhotos struct {
	Result struct {
		TotalCount int `json:"total_count"`
		Photos     [][]struct {
			FileID string `json:"file_id"`
		} `json:"photos"`
	} `json:"result"`
}

type FileResponse struct {
	Result struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// Пользователи
type User struct {
	UserID           int64     `json:"userID"`
	UserFirstName    string    `json:"userFirstName"`
	UserLastName     string    `json:"userLastName,omitempty"`
	Username         string    `json:"username,omitempty"`
	PhoneNumber      string    `json:"phoneNumber,omitempty"`
	RegistrationDate time.Time `json:"registrationDate"`
}

type UsersData struct {
	Users []User `json:"users"`
}

// Сообщения
type Message struct {
	UserID      int64     `json:"userID"`
	MessageID   int       `json:"messageID"`
	MediaGroupID string    `json:"mediaGroupID,omitempty"`
	Text        string    `json:"text,omitempty"`
	PhotoID     string    `json:"photoID,omitempty"`
	PhotoPath   string    `json:"photoPath,omitempty"`
	Distance    float64   `json:"distance"`
	MessageDate time.Time `json:"messageDate"`
}

type MessagesData struct {
	Messages []Message `json:"messages"`
}

// Загрузка конфига
func loadConfig(filename string) (Config, error) {
	var config Config
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return Config{}, err
	}
	return config, nil
}

// Работа с пользователями
func loadUsersData(filename string) (UsersData, error) {
	var usersData UsersData
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return UsersData{Users: []User{}}, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return UsersData{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&usersData)
	if err != nil {
		return UsersData{}, err
	}
	return usersData, nil
}

func saveUsersData(filename string, usersData UsersData) error {
	dir := filepath.Dir(filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(usersData)
}

// Работа с сообщениями
func loadMessagesData(filename string) (MessagesData, error) {
	var messagesData MessagesData
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return MessagesData{Messages: []Message{}}, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return MessagesData{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&messagesData)
	if err != nil {
		return MessagesData{}, err
	}
	return messagesData, nil
}

func saveMessagesData(filename string, messagesData MessagesData) error {
	dir := filepath.Dir(filename)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(messagesData)
}

// Сохранение фото (только самого большого размера)
func saveLargestPhoto(botToken string, photos []tgbotapi.PhotoSize) (string, string, error) {
	if len(photos) == 0 {
		return "", "", fmt.Errorf("нет фото для сохранения")
	}

	// Находим фото с самым большим разрешением
	var largestPhoto tgbotapi.PhotoSize
	for _, photo := range photos {
		if photo.Width*photo.Height > largestPhoto.Width*largestPhoto.Height {
			largestPhoto = photo
		}
	}

	// Получаем информацию о файле
	fileInfoURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", botToken, largestPhoto.FileID)
	resp, err := http.Get(fileInfoURL)
	if err != nil {
		return "", "", fmt.Errorf("ошибка getFile: %v", err)
	}
	defer resp.Body.Close()

	var fileResponse FileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResponse); err != nil {
		return "", "", fmt.Errorf("ошибка декодирования JSON: %v", err)
	}

	// Скачиваем файл
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, fileResponse.Result.FilePath)
	if _, err := os.Stat("photos"); os.IsNotExist(err) {
		os.Mkdir("photos", 0755)
	}

	resp, err = http.Get(fileURL)
	if err != nil {
		return "", "", fmt.Errorf("ошибка загрузки файла: %v", err)
	}
	defer resp.Body.Close()

	// Сохраняем файл
	filename := fmt.Sprintf("photos/%s_%d.jpg", largestPhoto.FileID, time.Now().Unix())
	out, err := os.Create(filename)
	if err != nil {
		return "", "", fmt.Errorf("ошибка создания файла: %v", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("ошибка записи файла: %v", err)
	}

	return largestPhoto.FileID, filename, nil
}

// Извлечение дистанции
func extractDistance(message string) float64 {
	cleaned := strings.ReplaceAll(message, " ", "")
	reKm := regexp.MustCompile(`(?i)#км`)
	if !reKm.MatchString(cleaned) {
		return -1
	}
	reNumbers := regexp.MustCompile(`\+(\d+[\.,]?\d*)`)
	matches := reNumbers.FindAllStringSubmatch(cleaned, -1)
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

func FloatToString(f float64) string {
	str := strconv.FormatFloat(f, 'f', 10, 64)
	str = strings.TrimRight(str, "0")
	str = strings.TrimRight(str, ".")
	return str
}

// Проверка, является ли пользователь администратором
func isAdmin(userID int64, config Config) bool {
	for _, adminID := range config.AdminIDs {
		if userID == adminID {
			return true
		}
	}
	return false
}

// Настройка CORS
func setupCORS(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// REST API
func setupRESTAPI(usersDataFile, messagesDataFile string) {
	http.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		setupCORS(&w)
		if r.Method == "OPTIONS" {
			return
		}

		usersData, err := loadUsersData(usersDataFile)
		if err != nil {
			http.Error(w, "Ошибка загрузки пользователей", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersData)
	})

	http.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
		setupCORS(&w)
		if r.Method == "OPTIONS" {
			return
		}

		messagesData, err := loadMessagesData(messagesDataFile)
		if err != nil {
			http.Error(w, "Ошибка загрузки сообщений", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messagesData)
	})

	log.Println("REST API запущен на http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}

// Сохранение аватара пользователя
func saveUserAvatar(botToken string, userID int64) (string, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getUserProfilePhotos?user_id=%d", botToken, userID))
	if err != nil {
		return "", fmt.Errorf("ошибка запроса getUserProfilePhotos: %v", err)
	}
	defer resp.Body.Close()

	var photos UserProfilePhotos
	if err := json.NewDecoder(resp.Body).Decode(&photos); err != nil {
		return "", fmt.Errorf("ошибка декодирования фото профиля: %v", err)
	}

	if photos.Result.TotalCount == 0 {
		return "", nil
	}

	fileID := photos.Result.Photos[0][0].FileID

	fileInfoURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", botToken, fileID)
	resp, err = http.Get(fileInfoURL)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса getFile: %v", err)
	}
	defer resp.Body.Close()

	var fileResponse FileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResponse); err != nil {
		return "", fmt.Errorf("ошибка декодирования file path: %v", err)
	}

	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botToken, fileResponse.Result.FilePath)
	resp, err = http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("ошибка загрузки аватара: %v", err)
	}
	defer resp.Body.Close()

	if _, err := os.Stat("photos"); os.IsNotExist(err) {
		os.Mkdir("photos", 0777)
	}

	filename := fmt.Sprintf("photos/avatar_%d.jpg", userID)
	out, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("ошибка создания файла аватара: %v", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка сохранения аватара: %v", err)
	}

	return filename, nil
}

// Обработка редактирования сообщения
func handleEditedMessage(bot *tgbotapi.BotAPI, config Config, usersDataFile, messagesDataFile string, editedMessage *tgbotapi.Message) {
	newText := editedMessage.Text
	if newText == "" {
		newText = editedMessage.Caption
	}

	distance := extractDistance(newText)
	if distance <= 0 {
		log.Printf("Сообщение #%d не содержит дистанции", editedMessage.MessageID)
		return
	}

	var photoID, photoPath string
	var err error
	if editedMessage.Photo != nil && len(editedMessage.Photo) > 0 {
		photoID, photoPath, err = saveLargestPhoto(bot.Token, editedMessage.Photo)
		if err != nil {
			log.Printf("Ошибка сохранения фото: %v", err)
		}
	}

	messagesData, _ := loadMessagesData(messagesDataFile)
	for i, msg := range messagesData.Messages {
		if msg.MessageID == editedMessage.MessageID {
			updatedMsg := Message{
				UserID:      editedMessage.From.ID,
				MessageID:   editedMessage.MessageID,
				MediaGroupID: editedMessage.MediaGroupID,
				Text:        newText,
				Distance:    distance,
				MessageDate: time.Now(),
			}
			if photoID != "" && photoPath != "" {
				// Удаляем старое фото, если оно было
				if msg.PhotoPath != "" {
					if err := os.Remove(msg.PhotoPath); err != nil {
						log.Printf("Ошибка удаления старого фото: %v", err)
					}
				}
				updatedMsg.PhotoID = photoID
				updatedMsg.PhotoPath = photoPath
			} else {
				// Сохраняем старые данные фото, если новые не были загружены
				updatedMsg.PhotoID = msg.PhotoID
				updatedMsg.PhotoPath = msg.PhotoPath
			}
			messagesData.Messages[i] = updatedMsg
			break
		}
	}
	saveMessagesData(messagesDataFile, messagesData)

	replyText := fmt.Sprintf("Обновлено: %s км", FloatToString(distance))
	msg := tgbotapi.NewMessage(editedMessage.Chat.ID, replyText)
	bot.Send(msg)
}

// Обработка медиагруппы
func handleMediaGroup(bot *tgbotapi.BotAPI, config Config, usersDataFile, messagesDataFile string, message *tgbotapi.Message) {
	text := message.Caption
	distance := extractDistance(text)
	if distance <= 0 {
		return
	}

	// Проверяем, есть ли уже сообщение с таким MediaGroupID
	messagesData, _ := loadMessagesData(messagesDataFile)
	var existingMessage *Message
	for i, msg := range messagesData.Messages {
		if msg.MediaGroupID == message.MediaGroupID {
			existingMessage = &messagesData.Messages[i]
			break
		}
	}

	// Сохраняем только самое большое фото из сообщения
	photoID, photoPath, err := saveLargestPhoto(bot.Token, message.Photo)
	if err != nil {
		log.Printf("Ошибка сохранения фото: %v", err)
		return
	}

	if existingMessage != nil {
		// Обновляем существующее сообщение, если новое фото больше
		existingPhotoSize := 0
		if existingMessage.PhotoID != "" {
			for _, ph := range message.Photo {
				if ph.FileID == existingMessage.PhotoID {
					existingPhotoSize = ph.Width * ph.Height
					break
				}
			}
		}

		newPhotoSize := message.Photo[len(message.Photo)-1].Width * message.Photo[len(message.Photo)-1].Height

		if newPhotoSize > existingPhotoSize {
			// Удаляем старое фото
			if existingMessage.PhotoPath != "" {
				if err := os.Remove(existingMessage.PhotoPath); err != nil {
					log.Printf("Ошибка удаления старого фото: %v", err)
				}
			}
			// Сохраняем новое
			existingMessage.PhotoID = photoID
			existingMessage.PhotoPath = photoPath
			saveMessagesData(messagesDataFile, messagesData)
		}
	} else {
		// Создаем новое сообщение
		messagesData.Messages = append(messagesData.Messages, Message{
			UserID:      message.From.ID,
			MessageID:   message.MessageID,
			MediaGroupID: message.MediaGroupID,
			Text:        text,
			PhotoID:     photoID,
			PhotoPath:   photoPath,
			Distance:    distance,
			MessageDate: time.Now(),
		})
		saveMessagesData(messagesDataFile, messagesData)

		// Отправляем ответ только для первого сообщения в группе
		reply := fmt.Sprintf(
			"Засчитано: %s км\nСтатистика: http://45.143.95.82:90/\nУдалить: /rm_%d",
			FloatToString(distance),
			message.MessageID,
		)
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, reply))
	}
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Panicf("Ошибка загрузки config.json: %v", err)
	}

	bot, err := tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = false
	log.Printf("Бот запущен: @%s", bot.Self.UserName)

	usersDataFile := "JSON/users_data.json"
	messagesDataFile := "JSON/messages_data.json"

	go setupRESTAPI(usersDataFile, messagesDataFile)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 10
	updates := bot.GetUpdatesChan(u)

	// Кэш для отслеживания обработанных медиагрупп
	processedMediaGroups := make(map[string]bool)

	for update := range updates {
		if update.Message != nil && update.Message.From.ID == bot.Self.ID {
			continue
		}

		// Обработка команды удаления
		if update.Message != nil && strings.HasPrefix(update.Message.Text, "/rm") {
			userID := update.Message.From.ID
			isAdmin := isAdmin(userID, config)

			parts := strings.SplitN(update.Message.Text, "_", 2)
			if len(parts) < 2 {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Формат: /rm_<ID>")
				bot.Send(msg)
				continue
			}

			messageID, err := strconv.Atoi(parts[1])
			if err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Некорректный ID")
				bot.Send(msg)
				continue
			}

			messagesData, _ := loadMessagesData(messagesDataFile)
			found := false
			for i, msg := range messagesData.Messages {
				if msg.MessageID == messageID {
					if msg.UserID == userID || isAdmin {
						// Удаляем связанное фото
						if msg.PhotoPath != "" {
							if err := os.Remove(msg.PhotoPath); err != nil {
								log.Printf("Ошибка удаления файла %s: %v", msg.PhotoPath, err)
							}
						}
						// Удаляем запись о сообщении
						messagesData.Messages = append(messagesData.Messages[:i], messagesData.Messages[i+1:]...)
						saveMessagesData(messagesDataFile, messagesData)
						reply := fmt.Sprintf("Удалено: #%d\n%s", messageID, msg.Text)
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, reply))
						found = true
					} else {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет прав для удаления этого сообщения"))
					}
					break
				}
			}

			if !found {
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Сообщение не найдено"))
			}
			continue
		}

		if update.EditedMessage != nil {
			handleEditedMessage(bot, config, usersDataFile, messagesDataFile, update.EditedMessage)
			continue
		}

		if update.Message == nil {
			continue
		}

		// Регистрация пользователя
		user := update.Message.From
		avatarPath, err := saveUserAvatar(bot.Token, user.ID)
		if err != nil {
			log.Printf("Ошибка сохранения аватара пользователя %d: %v", user.ID, err)
		} else if avatarPath != "" {
			log.Printf("Аватар пользователя %d сохранен: %s", user.ID, avatarPath)
		}

		usersData, _ := loadUsersData(usersDataFile)
		userExists := false
		for _, u := range usersData.Users {
			if u.UserID == user.ID {
				userExists = true
				break
			}
		}

		if !userExists {
			usersData.Users = append(usersData.Users, User{
				UserID:           user.ID,
				UserFirstName:    user.FirstName,
				UserLastName:     user.LastName,
				Username:         user.UserName,
				RegistrationDate: time.Now(),
			})
			saveUsersData(usersDataFile, usersData)
		}

		// Обработка медиагруппы
		if update.Message.MediaGroupID != "" {
			// Проверяем, обрабатывали ли мы уже эту медиагруппу
			if _, processed := processedMediaGroups[update.Message.MediaGroupID]; !processed {
				processedMediaGroups[update.Message.MediaGroupID] = true
				handleMediaGroup(bot, config, usersDataFile, messagesDataFile, update.Message)
			}
			continue
		}

		// Обработка одиночного сообщения с фото
		text := update.Message.Text
		if text == "" {
			text = update.Message.Caption
		}

		distance := extractDistance(text)
		if distance <= 0 {
			continue
		}

		if update.Message.Photo == nil {
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Прикрепите скрин трека"))
			continue
		}

		// Сохраняем только самое большое фото
		photoID, photoPath, err := saveLargestPhoto(bot.Token, update.Message.Photo)
		if err != nil {
			log.Printf("Ошибка сохранения фото: %v", err)
			continue
		}

		messagesData, _ := loadMessagesData(messagesDataFile)
		messagesData.Messages = append(messagesData.Messages, Message{
			UserID:      user.ID,
			MessageID:   update.Message.MessageID,
			Text:        text,
			PhotoID:     photoID,
			PhotoPath:   photoPath,
			Distance:    distance,
			MessageDate: time.Now(),
		})
		saveMessagesData(messagesDataFile, messagesData)

		reply := fmt.Sprintf(
			"Засчитано: %s км\nСтатистика: http://45.143.95.82:90/\nУдалить: /rm_%d",
			FloatToString(distance),
			update.Message.MessageID,
		)
		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, reply))
	}
}