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

// Обновленная структура для заявки в соответствии с ТЗ
type Application struct {
 ID        string    `json:"id"`
 OrgName   string    `json:"org_name"`
 INN       string    `json:"inn"`
 FullName  string    `json:"full_name"` // ФИО
 Position  string    `json:"position"`  // Должность
 Phone     string    `json:"phone"`     // Телефон
 Email     string    `json:"email"`     // Корпоративный email
 CreatedAt time.Time `json:"created_at"`
}

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
  return calcCheckDigit(d11, nums) == nums[10] && calcCheckDigit(d12, nums) == nums[11]
 }
}

func sendRealEmail(fromEmail, appPassword, to, subject, body string) {
 smtpHost := "smtp.yandex.ru"
 smtpPort := "587"

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

 err := smtp.SendMail(smtpHost+":"+smtpPort, auth, fromEmail, []string{to}, []byte(message))
 if err != nil {
  log.Printf("[Ошибка SMTP] Не удалось отправить письмо на %s: %v", to, err)
  return
 }
 log.Printf("[Успех SMTP] Письмо успешно отправлено на %s", to)
}

func main() {
 // ================= НАСТРОЙКА ПОЧТЫ =================
 smtpEmail := ""    // Ваша техническая почта-отправитель
 smtpPassword := "" // Ваш 16-значный пароль приложения
 adminEmail := ""   // Почта администратора (куда придут данные формы)
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
   fullName := r.FormValue("full_name")
   position := r.FormValue("position")
   phone := r.FormValue("phone")
   email := r.FormValue("email")

   // Проверка на заполненность всех новых полей
   if orgName == "" || inn == "" || fullName == "" || position == "" || phone == "" || email == "" {
    tmpl.ExecuteTemplate(w, "index.html", "Все поля обязательны для заполнения.")
    return
   }

   if !validateINN(inn) {
    tmpl.ExecuteTemplate(w, "index.html", "Некорректный ИНН (контрольная сумма не совпадает).")
    return
   }

   app := Application{
    ID:        fmt.Sprintf("%x", sha256.Sum256([]byte(inn+time.Now().String())))[:8],
    OrgName:   orgName,
    INN:       inn,
    FullName:  fullName,
    Position:  position,
    Phone:     phone,
    Email:     email,
    CreatedAt: time.Now(),
   }

   storage.Add(app)

   // Асинхронная фоновая отправка (горутина)
   go func(a Application) {
    if smtpEmail == "" || smtpPassword == "" {
     log.Println("[Внимание] Параметры SMTP не настроены. Письма симулируются в консоли.")
     return
    }
    //Направление ПОЛНЫХ данных формы на е-мейл администратора
    adminBody := fmt.Sprintf(`
     <h3 style="color: #eb5a00;">Зафиксирована новая заявка на демо-доступ</h3>
     <table border="1" cellpadding="8" style="border-collapse: collapse; font-family: sans-serif;">
      <tr style="background-color: #f4f6f9;"><td><b>Параметр</b></td><td><b>Значение</b></td></tr>
      <tr><td><b>Организация</b></td><td>%s</td></tr>
      <tr><td><b>ИНН</b></td><td>%s</td></tr>
      <tr><td><b>ФИО заявителя</b></td><td>%s</td></tr>
      <tr><td><b>Должность</b></td><td>%s</td></tr>
      <tr><td><b>Контактный телефон</b></td><td>%s</td></tr>
      <tr><td><b>Email пользователя</b></td><td><a href="mailto:%s">%s</a></td></tr>
      <tr><td><b>Дата отправки</b></td><td>%s</td></tr>
     </table>
    `, a.OrgName, a.INN, a.FullName, a.Position, a.Phone, a.Email, a.Email, a.CreatedAt.Format("02.01.2006 15:04:05"))

    sendRealEmail(smtpEmail, smtpPassword, adminEmail, "Новая заявка: Демо-доступ", adminBody)

   }(app)

   tmpl.ExecuteTemplate(w, "success.html", email)
  }
 })

 http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
  apps := storage.GetAll()
  tmpl.ExecuteTemplate(w, "admin.html", apps)
 })

 fmt.Println("Сервер запущен на http://localhost:8080")
 log.Fatal(http.ListenAndServe(":8080", nil))
}