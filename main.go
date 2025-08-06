package main

import (
	"bufio"
	"github.com/joho/godotenv"
	"github.com/zelenin/go-tdlib/client"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func extractPaths(output string) []string {
	var paths []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		prefix := "✓  Image downloaded successfully to: "
		if strings.HasPrefix(line, prefix) {
			path := strings.TrimPrefix(line, prefix)
			paths = append(paths, path)
		}
	}

	return paths
}

func main() {
	env_err := godotenv.Load()
	if env_err != nil {
		log.Fatal("Ошибка загрузки .env файла")
	}
	apiIDInt, _ := strconv.Atoi(os.Getenv("TG_API_ID"))
	params := &client.SetTdlibParametersRequest{
		UseTestDc:           false,
		DatabaseDirectory:   "./tdlib-db",
		FilesDirectory:      "./tdlib-files",
		UseFileDatabase:     true,
		UseChatInfoDatabase: true,
		UseMessageDatabase:  true,
		UseSecretChats:      false,
		ApiId:               int32(apiIDInt),
		ApiHash:             os.Getenv("TG_API_HASH"),
		SystemLanguageCode:  "en",
		DeviceModel:         "PC",
		SystemVersion:       "Linux",
		ApplicationVersion:  "0.1",
	}

	// Авторизация
	authorizer := client.ClientAuthorizer(params)

	go client.CliInteractor(authorizer) // Ждёт ввод телефона, кода, пароля

	var tdlibClient *client.Client
	var err error

	client.SetLogVerbosityLevel(&client.SetLogVerbosityLevelRequest{
		NewVerbosityLevel: 0,
	})

	// Создаём клиент в отдельной горутине
	done := make(chan struct{})
	go func() {
		tdlibClient, err = client.NewClient(authorizer)
		close(done)
	}()

	// Ждём создания клиента
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		log.Fatal("⏱️ Превышено время ожидания клиента")
	}

	if err != nil {
		log.Fatalf("❌ Ошибка создания клиента: %v", err)
	}

	// Получение своих данных
	me, err := tdlibClient.GetMe()
	if err != nil {
		log.Fatalf("❌ Ошибка GetMe: %v", err)
	}
	log.Printf("✅ Вошли как %s (%d)", me.FirstName, me.Id)

	// Подписка на апдейты
	listener := tdlibClient.GetListener()
	defer listener.Close()

	for update := range listener.Updates {

		switch u := update.(type) {

		case *client.UpdateFile:
			if u.File.Remote.IsUploadingCompleted {
				log.Printf("Файл загружен[%s]", u.File.Local.Path)
				if _, err := os.Stat(u.File.Local.Path); err == nil {
					os.Remove(u.File.Local.Path)
					log.Printf("Был удален локальный файл:" + u.File.Local.Path)
				}
			}

		case *client.UpdateNewMessage:
			message := u.Message
			senderUser, ok := message.SenderId.(*client.MessageSenderUser)
			if !ok || senderUser.UserId != me.Id {
				continue
			}
			textMsg, ok := message.Content.(*client.MessageText)
			if !ok {
				continue
			}

			msgText := strings.TrimSpace(textMsg.Text.Text)
			log.Printf("➡️ Моё сообщение: '%s'", msgText)

			if strings.Contains(strings.ToLower(msgText), "https://vt.tiktok.com/") {
				error := false

				tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "⏳ Скачиваю...",
						},
					},
				})

				cmd := exec.Command("tiktokdl", "download", msgText)
				output, err := cmd.CombinedOutput()
				if err != nil {
					error = true
				}

				if !error {
					outputStr := string(output)
					re := regexp.MustCompile(`Video downloaded successfully to:\s*(.*\.mp4)`)
					match := re.FindStringSubmatch(outputStr)
					if len(match) >= 2 {
						filePath := match[1]
						log.Printf("Загружен файл: %s", filePath)
						log.Printf("Отправка[%s] в чат[%d]", filePath, message.ChatId)
						tdlibClient.SendMessage(&client.SendMessageRequest{
							ChatId: message.ChatId,
							InputMessageContent: &client.InputMessageVideo{
								Video: &client.InputFileLocal{
									Path: filePath,
								},
							},
						})

					} else {
						images := extractPaths(outputStr)
						if len(images) == 0 {
							error = true
						} else {
							var imagesTelegramContent []client.InputMessageContent
							for _, image := range images {
								imagesTelegramContent = append(imagesTelegramContent, &client.InputMessagePhoto{
									Photo: &client.InputFileLocal{
										Path: image,
									},
									Caption: &client.FormattedText{
										Text: "",
									},
								})
							}

							_, err := tdlibClient.SendMessageAlbum(&client.SendMessageAlbumRequest{
								ChatId:               message.ChatId,
								InputMessageContents: imagesTelegramContent,
							})

							if err != nil {
								tdlibClient.SendMessage(&client.SendMessageRequest{
									ChatId: message.ChatId,
									InputMessageContent: &client.InputMessageText{
										Text: &client.FormattedText{
											Text: "Не удалось отправить фотографии!",
										},
									},
								})
							}
						}
					}
				}

				if error {
					tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: "Не удалось скачать видео - " + msgText,
							},
						},
					})
				} else {
					tdlibClient.DeleteMessages(&client.DeleteMessagesRequest{
						ChatId:     message.ChatId,
						MessageIds: []int64{message.Id},
						Revoke:     true,
					})
				}
			}

			if msgText == "!love" {
				_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🤍🤍🤍🤍🤍🤍🤍🤍🤍🤍] 0%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				loadingAnimFrames := []string{
					"[❤️🤍🤍🤍🤍🤍🤍🤍🤍🤍] 10%",
					"[❤❤🤍🤍🤍🤍🤍🤍🤍🤍] 20%",
					"[❤❤❤🤍🤍🤍🤍🤍🤍🤍] 30%",
					"[❤❤❤❤🤍🤍🤍🤍🤍🤍] 40%",
					"[❤❤❤❤❤🤍🤍🤍🤍🤍] 50%",
					"[❤❤❤❤❤❤🤍🤍🤍🤍] 60%",
					"[❤❤❤❤❤❤❤🤍🤍🤍] 70%",
					"[❤❤❤❤❤❤❤❤🤍🤍] 80%",
					"[❤❤❤❤❤❤❤❤❤🤍] 90%",
					"[❤❤❤❤❤❤❤❤❤❤] 100%",
				}

				for _, value := range loadingAnimFrames {

					_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: value,
							},
						},
					})

					if err != nil {
						log.Printf("❌ Ошибка редактирования: %v", err)
					} else {
						log.Printf("Success")
					}

					time.Sleep(200 * time.Millisecond)
				}

				constructScreenAnimFrames := []string{
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
				}

				for _, value := range constructScreenAnimFrames {
					// time.Sleep(1 * time.Second)
					_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: value,
							},
						},
					})

					if err != nil {
						log.Printf("❌ Ошибка редактирования: %v", err)
					} else {
						log.Printf("Success")
					}
				}

				mainAnimeFrames := []string{
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️🟥⬜️🟥⬜️⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️⬜️🟥🟥🟥⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️🟥⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️🟥⬜️🟥⬜️⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️⬜️🟥🟥🟥⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️🟥⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️🟥⬜️🟥⬜️⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️⬜️🟥🟥🟥⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️🟥⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️🟥⬜️🟥⬜️⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️🟥🟥🟥🟥🟥⬜️⬜️\n⬜️⬜️⬜️🟥🟥🟥⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️🟥⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
				}

				for _, value := range mainAnimeFrames {
					time.Sleep(300 * time.Millisecond)
					_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: value,
							},
						},
					})

					if err != nil {
						log.Printf("❌ Ошибка редактирования: %v", err)
					} else {
						log.Printf("Success")
					}
				}

				destroyScreenAnimFrames := []string{
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️❤️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️\n⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
					"⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️⬜️",
				}

				for _, value := range destroyScreenAnimFrames {
					//time.Sleep(300 * time.Millisecond)
					_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: value,
							},
						},
					})

					if err != nil {
						log.Printf("❌ Ошибка редактирования: %v", err)
					} else {
						log.Printf("Success")
					}
				}

				finalAnimFrames := []string{
					"Я тебя люблю",
					"❤️ тебя люблю",
					"Я❤️тебя люблю",
					"Я ❤️ебя люблю",
					"Я т❤️бя люблю",
					"Я те❤️я люблю",
					"Я теб❤️ люблю",
					"Я тебя❤️люблю",
					"Я тебя ❤️юблю",
					"Я тебя л❤️блю",
					"Я тебя лю❤️лю",
					"Я тебя люб❤️ю",
					"Я тебя любл❤️",
					"Я тебя люблю❤️",
				}

				for _, value := range finalAnimFrames {
					//time.Sleep(300 * time.Millisecond)
					_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
						ChatId:    message.ChatId,
						MessageId: message.Id,
						InputMessageContent: &client.InputMessageText{
							Text: &client.FormattedText{
								Text: value,
							},
						},
					})

					if err != nil {
						log.Printf("❌ Ошибка редактирования: %v", err)
					} else {
						log.Printf("Success")
					}
				}

			}

			if msgText == "!Z" {
				_, err := tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "Происходит инициализация программы руссифкации...",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(3 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦] 0%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(1 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦] 10%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(1 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇷🇺🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦🇺🇦] 20%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(1 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇺🇦🇺🇦🇺🇦🇺🇦] 60%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(1 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇺🇦🇺🇦🇺🇦] 70%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(3 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇺🇦] 90%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(5 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "[🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺🇷🇺] 100%",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}

				time.Sleep(2 * time.Second)

				_, err = tdlibClient.EditMessageText(&client.EditMessageTextRequest{
					ChatId:    message.ChatId,
					MessageId: message.Id,
					InputMessageContent: &client.InputMessageText{
						Text: &client.FormattedText{
							Text: "Новая область в России успешно создана!",
						},
					},
				})

				if err != nil {
					log.Printf("❌ Ошибка редактирования: %v", err)
				} else {
					log.Printf("Success")
				}
			}
		}
	}
}
