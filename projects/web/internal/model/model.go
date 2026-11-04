// Package model describes gateway routing and session policies using plain
// values, without depending on HTTP objects, clients or serializers.
package model

type Target string

const (
	Accounts Target = "accounts"
	Queue    Target = "queue"
)

type Operation string

const (
	Authenticate Operation = "authenticate"
	Proxy        Operation = "proxy"
	Logout       Operation = "logout"
)

type Route struct {
	Method        string
	Path          string
	Target        Target
	Operation     Operation
	Authenticated bool
}

// Request carries values, never HTTP objects or arbitrary browser headers.
type Request struct {
	Method         string
	Path           string
	RawPath        string
	Query          string
	Body           []byte
	ContentType    string
	Token          string
	IdempotencyKey string
}

type Response struct {
	Status int
	Body   []byte
}

type User struct {
	ID      int64
	Name    string
	Email   string
	Role    string
	GroupID *int64
}

type AuthExchange struct {
	Response Response
	Token    string
	User     *User
}

type Session struct {
	Token         string
	MaxAgeSeconds int
}

// Result separates the browser-safe response from cookie-only credentials.
type Result struct {
	Response Response
	User     *User
	Session  *Session
}

type Fault struct {
	Status  int
	Code    string
	Message string
}

func (e *Fault) Error() string { return e.Message }

func Unauthorized() *Fault {
	return &Fault{Status: 401, Code: "unauthorized", Message: "Войдите в систему"}
}
func NotFound() *Fault {
	return &Fault{Status: 404, Code: "not_found", Message: "Маршрут не найден"}
}
func Unavailable() *Fault {
	return &Fault{Status: 502, Code: "upstream_unavailable", Message: "Сервис временно недоступен. Попробуйте еще раз"}
}
func InvalidResponse() *Fault {
	return &Fault{Status: 502, Code: "upstream_response", Message: "Сервис вернул некорректный ответ"}
}
func InvalidAuthentication() *Fault {
	return &Fault{Status: 502, Code: "upstream_response", Message: "Сервис входа вернул некорректный ответ"}
}
func NotReady() *Fault {
	return &Fault{Status: 503, Code: "not_ready", Message: "Сервисы еще не готовы"}
}
