package credman

import (
	"crypto/rand"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

var (
	TestCookie types.Cookie
	cm         *CookieManager
	key        []byte
)

func TestMain(m *testing.M) {
	TestCookie = types.Cookie{
		Name:     "test_cookie",
		Value:    "test_value",
		Domain:   "example.com",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	}

	var err error
	key = make([]byte, 32)
	_, err = rand.Read(key)
	if err != nil {
		panic(err)
	}

	cm, err = NewCookieManager("cookies.warp", key)
	if err != nil {
		panic(err)
	}

	exitCode := m.Run()
	cm.Close()
	os.Remove("cookies.warp")
	os.Exit(exitCode)
}

func TestAddCookie(t *testing.T) {
	err := cm.SetCookie(TestCookie)
	if err != nil {
		t.Errorf("Error adding cookie: %v", err)
	}

	cookie, err := cm.GetCookie("test_cookie")
	if err != nil {
		t.Errorf("Error getting cookie: %v", err)
		return
	}
	compareCookies(t, &TestCookie, cookie)
}

func TestRemoveCookie(t *testing.T) {
	err := cm.SetCookie(TestCookie)
	if err != nil {
		t.Errorf("Error adding cookie: %v", err)
		return
	}

	err = cm.DeleteCookie("test_cookie")
	if err != nil {
		t.Errorf("Error removing cookie: %v", err)
	}

	_, err = cm.GetCookie("test_cookie")
	if err == nil {
		t.Errorf("Cookie was not deleted")
	}
}

func TestUpdateCookie(t *testing.T) {
	err := cm.SetCookie(TestCookie)
	if err != nil {
		t.Errorf("Error adding cookie: %v", err)
		return
	}

	updatedCookie := TestCookie
	updatedCookie.Value = "updated_value"
	err = cm.UpdateCookie(&updatedCookie)
	if err != nil {
		t.Errorf("Error updating cookie: %v", err)
		return
	}

	cookie, err := cm.GetCookie("test_cookie")
	if err != nil {
		t.Errorf("Error getting cookie: %v", err)
		return
	}
	compareCookies(t, &updatedCookie, cookie)
}

func compareCookies(t *testing.T, expected *types.Cookie, actual *types.Cookie) {
	expectedValue := reflect.ValueOf(expected).Elem()
	actualValue := reflect.ValueOf(actual).Elem()

	for i := 0; i < expectedValue.NumField(); i++ {
		expectedField := expectedValue.Field(i)
		actualField := actualValue.Field(i)

		if !reflect.DeepEqual(expectedField.Interface(), actualField.Interface()) {
			t.Errorf("Expected %s %v, got %v", expectedValue.Type().Field(i).Name, expectedField.Interface(), actualField.Interface())
		}
	}
}
