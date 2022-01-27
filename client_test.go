package main

import (
	"encoding/json"
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const accessToken = "abc-def"

type XMLRoot struct {
	XMLName xml.Name `xml:"root"`
	Rows    []XMLRow `xml:"row"`
}

type XMLRow struct {
	XMLName   xml.Name `xml:"row"`
	Id        int      `xml:"id"`
	FirstName string   `xml:"first_name"`
	LastName  string   `xml:"last_name"`
	Age       int      `xml:"age"`
	About     string   `xml:"about"`
	Gender    string   `xml:"gender"`
}

func SearchServer(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("AccessToken") != accessToken {
		http.Error(w, "Bad access token", http.StatusUnauthorized)
		return
	}

	file, err := os.Open("dataset.xml")
	if err != nil {
		http.Error(w, "file opening failed", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	var data XMLRoot

	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		http.Error(w, "file reading failed", http.StatusInternalServerError)
		return
	}
	xml.Unmarshal(fileContent, &data)

	q := r.URL.Query()

	query := q.Get("query")
	var users []User

	for _, el := range data.Rows {
		if query != "" {
			if !(strings.Contains(el.About, query) ||
				strings.Contains(el.FirstName, query) || strings.Contains(el.LastName, query)) {
				continue
			}
		}
		users = append(users, User{
			Id:     el.Id,
			Age:    el.Age,
			Gender: el.Gender,
			About:  el.About,
			Name:   el.FirstName + " " + el.LastName,
		})
	}

	orderBy, _ := strconv.Atoi(q.Get("order_by"))

	if orderBy != OrderByAsIs {
		orderField := q.Get("order_field")
		var f func(lhs User, rhs User) bool
		switch orderField {
		case "Id":
			f = func(lhs User, rhs User) bool {
				return lhs.Id < rhs.Id
			}
		case "Name", "":
			f = func(lhs User, rhs User) bool {
				return lhs.Name < rhs.Name
			}
		case "Age":
			f = func(lhs User, rhs User) bool {
				return lhs.Age < rhs.Age
			}
		default:
			result, _ := json.Marshal(SearchErrorResponse{"ErrorBadOrderField"})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write(result)
			return
		}
		sort.Slice(users, func(i, j int) bool {
			return f(users[i], users[j]) && (orderBy == OrderByDesc)
		})
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit > 0 {
		from := offset
		if from > len(users)-1 {
			users = []User{}
		} else {
			to := offset + limit
			if to > len(users) {
				to = len(users)
			}

			users = users[from:to]
		}
	}

	result, err := json.Marshal(users)
	if err != nil {
		http.Error(w, "data marshalling failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

func newTestServer(token string) (*httptest.Server, SearchClient) {
	server := httptest.NewServer(http.HandlerFunc(SearchServer))
	client := SearchClient{token, server.URL}
	return server, client
}
func TestInvalidAccessToken(t *testing.T) {
	server, client := newTestServer("")
	defer server.Close()

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "Bad AccessToken" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestInvalidLowLimit(t *testing.T) {
	server, client := newTestServer(accessToken)
	defer server.Close()

	_, err := client.FindUsers(SearchRequest{Limit: -3})

	if err.Error() != "limit must be > 0" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestInvalidHighLimit(t *testing.T) {
	server, client := newTestServer(accessToken)
	defer server.Close()

	r, _ := client.FindUsers(SearchRequest{Limit: 26})

	if len(r.Users) != 25 {
		t.Errorf("Error : invalid number of users - %v", len(r.Users))
	}
}

func TestInvalidLowOffset(t *testing.T) {
	server, client := newTestServer(accessToken)
	defer server.Close()

	_, err := client.FindUsers(SearchRequest{Offset: -3})

	if err.Error() != "offset must be > 0" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestInvalidOrderField(t *testing.T) {
	server, client := newTestServer(accessToken)
	defer server.Close()

	_, err := client.FindUsers(SearchRequest{OrderBy: OrderByAsc, OrderField: "invalid"})

	if err.Error() != "OrderFeld invalid invalid" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestStatusInternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := json.Marshal(make(chan int))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "SearchServer fatal error" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestJSONUnpackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result []byte
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(result)
		return
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "cant unpack error json: unexpected end of JSON input" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestUnknownBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, _ := json.Marshal(SearchErrorResponse{"unknown bad request"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(result)
		return
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{OrderBy: OrderByAsc, OrderField: "unknown"})

	if err.Error() != "unknown bad request error: unknown bad request" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestJSONUnpackResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result []byte
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "cant unpack result json: unexpected end of JSON input" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestLenResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var users [30]User
		result, _ := json.Marshal(users)
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{Limit: 26})

	if err != nil {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
	}))
	defer server.Close()
	client := SearchClient{accessToken, server.URL}

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "timeout for limit=1&offset=0&order_by=0&order_field=&query=" {
		t.Errorf("Error : %v", err.Error())
	}
}

func TestUnknownError(t *testing.T) {
	client := SearchClient{accessToken, "unknown server"}

	_, err := client.FindUsers(SearchRequest{})

	if err.Error() != "unknown error Get \"unknown%20server?limit=1&offset=0&order_by=0&order_field=&query=\": unsupported protocol scheme \"\"" {
		t.Errorf("Error : %v", err.Error())
	}
}
