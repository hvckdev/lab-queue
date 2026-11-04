package infra

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const TokenTTL = 8 * time.Hour

type User struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Role    string `json:"role"`
	GroupID *int64 `json:"group_id"`
}

type Claims struct {
	User User `json:"user"`
	jwt.RegisteredClaims
}

type Tokens struct{ secret []byte }

func NewTokens(secret string) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	return &Tokens{secret: []byte(secret)}, nil
}

func (t *Tokens) Issue(user User) (string, error) {
	now := time.Now()
	claims := Claims{User: user, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "labqueue-accounts", Audience: jwt.ClaimStrings{"labqueue"}, Subject: strconv.FormatInt(user.ID, 10), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

func (t *Tokens) Parse(token string) (User, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) { return t.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("labqueue-accounts"), jwt.WithAudience("labqueue"), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return User{}, fmt.Errorf("invalid token")
	}
	knownRole := claims.User.Role == "student" || claims.User.Role == "teacher" || claims.User.Role == "admin"
	validGroup := claims.User.Role == "student" && claims.User.GroupID != nil && *claims.User.GroupID > 0 || claims.User.Role != "student" && claims.User.GroupID == nil
	if claims.User.ID <= 0 || claims.Subject != strconv.FormatInt(claims.User.ID, 10) || !knownRole || !validGroup {
		return User{}, errors.New("invalid identity")
	}
	return claims.User, nil
}
