package main

import (
	"crypto/sha256"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/smtp"
	"sync"
	"time"
)

// Структура для заявки
type Application struct {
	ID        string    `json:"id"`
	OrgName   string    `json:"org_name"`
	INN       string    `json:"inn"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Хранилище ТОЛЬКО в оперативной памяти (без файлов)
type RAMStorage struct {
	mu    sync.Mutex
	items []Application
}

func (s *RAMStorage) Add(app Application) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, app)
}

func (s *RAMStorage) GetAll() []Application {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]Application, len(s.items))
	copy(res, s.items)
	return res
}

var storage = &RAMStorage{}

// Валидация ИНН (10 или 12 цифр)
func validateINN(inn string) bool {
	if len(inn) != 10 && len(inn) != 12 {
		return false
	}
	for _, r := range inn {
		if r < '0' || r > '9' {
			return false
		}
	}
	d10 := []int{2, 4, 10, 3, 5, 9, 4, 6, 8}
	d11 := []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}
	d12 := []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}

	nums := make([]int, len(inn))
	for i, r := range inn {
		nums[i] = int(r - '0')
	}

	calcCheckDigit := func(coefficients []int, digits []int) int {
		sum := 0
		for i, c := range coefficients {
			sum += c * digits[i]
		}
		return (sum % 11) % 10
	}

	if len(inn) == 10 {
		return calcCheckDigit(d10, nums) == nums[9]
	} else {
		check11 := calcCheckDigit(d11, nums)
		check12 := calcCheckDigit(d12, nums)
		return check11 == nums[10] && check12 == nums[11]
	}
}

// Настоящая отправка email через SMTP
func sendRealEmail(fromEmail, appPassword, to, subject, body string) {
	smtpHost := "smtp.yandex.ru" // Для Mail.ru: smtp.mail.ru
	smtpPort := "587"            // Порт для STARTTLS

	// Настройка заголовков, чтобы поддерживался русский язык и HTML
	header := make(map[string]string)
	header["From"] = fromEmail
	header["To"] = to
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/html; charset=\"UTF-8\""

	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	auth := smtp.PlainAuth("", fromEmail, appPassword, smtpHost)

	// Отправка письма
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, fromEmail, []string{to}, []byte(message))
	if err != nil {
		log.Printf("[Ошибка SMTP] Не удалось отправить письмо на %s: %v", to, err)
		return
	}
	log.Printf("[Успех SMTP] Письмо успешно отправлено на %s", to)
}

func main() {
	// ================= НАСТРОЙКА ПОЧТЫ =================
	// Впишите сюда свои данные, чтобы письма уходили по-настоящему:
	smtpEmail := "" // Ваша почта-отправитель
	smtpPassword := ""   // Ваш пароль приложения из настроек безопасности
	adminEmail := "" // Почта админа, куда слать уведомления о новых юзерах
	// ===================================================

	tmpl := template.Must(template.ParseGlob("templates/*.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tmpl.ExecuteTemplate(w, "index.html", nil)
			return
		}

		if r.Method == http.MethodPost {
			orgName := r.FormValue("org_name")
			inn := r.FormValue("inn")
			email := r.FormValue("email")

			if orgName == "" || inn == "" || email == "" {
				tmpl.ExecuteTemplate(w, "index.html", "Все поля обязательны для заполнения.")
				return
			}

			if !validateINN(inn) {
				tmpl.ExecuteTemplate(w, "index.html", "Некорректный ИНН (контрольная сумма не совпадает).")
				return
			}

			// Создаем запись в памяти
			app := Application{
				ID:        fmt.Sprintf("%x", sha256.Sum256([]byte(inn+time.Now().String())))[:8],
				OrgName:   orgName,
				INN:       inn,
				Email:     email,
				CreatedAt: time.Now(),
			}

			// Сохраняем в оперативную память (после перезапуска сервера сотрется)
			storage.Add(app)
			go func(a Application) {
				if smtpEmail == "your-email@yandex.ru" {
					log.Println("[Внимание] Вы не изменили настройки почты в main.go! Письма отправлены в консоль.")
					return
				}

				demoLink := fmt.Sprintf("https://demo.yoursystem.ru/access?token=%s", a.ID)

				// 1. Письмо пользователю
				userBody := fmt.Sprintf(`
     <h3>Добро пожаловать!</h3>
     <p>Вы успешно запросили демо-доступ для организации <b>%s</b> (ИНН: %s).</p>
     <p>Ваша персональная ссылка для входа: <a href="%s">%s</a></p>
    `, a.OrgName, a.INN, demoLink, demoLink)

				sendRealEmail(smtpEmail, smtpPassword, a.Email, "Доступ к демо-системе", userBody)

				// 2. Письмо администратору
				adminBody := fmt.Sprintf(`
     <h3>Зафиксирована новая авторизация в демо-системе</h3>
     <ul>
      <li><b>Организация:</b> %s</li>
      <li><b>ИНН:</b> %s</li>
      <li><b>Email пользователя:</b> %s</li>
      <li><b>Дата:</b> %s</li>
     </ul>
    `, a.OrgName, a.INN, a.Email, a.CreatedAt.Format("02.01.2006 15:04:05"))

				sendRealEmail(smtpEmail, smtpPassword, adminEmail, "Новая регистрация в демо", adminBody)
			}(app)

			tmpl.ExecuteTemplate(w, "success.html", email)
		}
	})

	// Страница админа по-прежнему работает, но читает данные из оперативной памяти
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		apps := storage.GetAll()
		tmpl.ExecuteTemplate(w, "admin.html", apps)
	})

	fmt.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Асинхронная отправка писем в фон
