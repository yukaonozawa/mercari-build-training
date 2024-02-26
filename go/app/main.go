package main

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"database/sql"
	"strconv"
	"mime/multipart"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	_ "github.com/mattn/go-sqlite3"
)

type Item struct {
	ID			int 	`json:"id"`
	Name 		string 	`json:"name"`
	Category 	string 	`json:"category"`
	ImageName 	string 	`json:"image"`
}
type Category struct {
	ID			int 	`json:"id"`
	Category 	string 	`json:"category_id"`
}

type Items struct {
	Items []Item `json:"items"`
}

const (
	ImgDir = "images"
	ItemsJson = "app/items.json"
	DBPath = "../db/mercari.sqlite3"
	SelectAllQuery = "SELECT items.id, items.name, categories.name AS category, items.image_name FROM items INNER JOIN categories ON items.category_id = categories.id"
)

type Response struct {
	Message string `json:"message"`
}

func root(c echo.Context) error {
	res := Response{Message: "Hello, world!"}
	return c.JSON(http.StatusOK, res)
}

func errorHandler(err error, c echo.Context, code int, message string) *echo.HTTPError {
	return echo.NewHTTPError (code, message)
}

func readItemsFromDB() ([]Item, error) {
	db, err := sql.Open("sqlite3", DBPath)
	if err != nil {
		return nil, fmt.Errorf("Could not open sqlite db file")
	}
	defer db.Close()
	
	rows, err := db.Query(SelectAllQuery)
	if err != nil{
		return nil, fmt.Errorf("Something went wrong with the query selecting all items ")
	}

	var items []Item

	for rows.Next() {
		var item Item
	
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.ImageName); err != nil {
			return nil, fmt.Errorf("Could not scan information from db file rows")
		}
		items = append(items, item)
	}

	return items, nil
}

func getCategoryID(db *sql.DB, category string) (int, error) {
	var catID int
	row, err := db.Query("SELECT id FROM categories WHERE name LIKE (?)", category)
	if err != nil{
		return 0, fmt.Errorf("Could not open sql db: %v", err)
	}
	defer row.Close()

	if !row.Next(){
		_, err = db.Exec("INSERT INTO categories (name) VALUES (?)", category)
		if err != nil {
			return 0, fmt.Errorf("Could insert new category: %v", err)
		}
		row, err = db.Query("SELECT id FROM categories WHERE name LIKE (?)", category)
		if err != nil{
			return 0, fmt.Errorf("Could not get id of new category: %v", err)
		}
		defer row.Close()
	} 
	
	for row.Next() {
		if err := row.Scan(&catID); err != nil {
			return 0, fmt.Errorf("Could not scan category ID number into catID")
		}	
	}
	
	return catID, err
}

func getItems(c echo.Context) error {
	items, err := readItemsFromDB()
	if err != nil {
		return errorHandler(err, c, http.StatusInternalServerError, "Could not read items from DB")
	}	
	return c.JSON(http.StatusOK, Items{Items:items})
}

func addItem(c echo.Context) error {
	// Get form data
	name := c.FormValue("name")
	category := c.FormValue("category")
	image, err := c.FormFile("image")
	if err != nil {
		return errorHandler(err, c, http.StatusBadRequest, "Image not found")
	}
	c.Logger().Debugf("Received item. Name: %s, Category: %s", name, category)

	hashString, err := imageHasher(image)
	if err != nil {
		c.Logger().Debugf("Error hashing image %v", err)
		return errorHandler(err, c, http.StatusInternalServerError, "Could not hash image")
	}

	if err = addItemtoDB(name, category, hashString); err != nil {
		c.Logger().Debugf("Error adding to db: %v", err)
		return errorHandler(err, c, http.StatusInternalServerError, "Could not add item" + name + category + hashString)
	}

	message := fmt.Sprintf("item received: %s, %s", name, category)
	res := Response{Message: message}

	return c.JSON(http.StatusOK, res)
}

func addItemtoDB(name string, category string, image string) error {
	db, err := sql.Open("sqlite3", DBPath)
	if err != nil {
		return fmt.Errorf("Could not open sql db")
	}
	defer db.Close() 
	
	catID, err := getCategoryID(db, category)
	if err != nil {
		return fmt.Errorf("Could not get catID: %v", err)
	}

	_, err = db.Exec("INSERT INTO items (name, category_id, image_name) VALUES (?,?,?)", name, catID, image)
	if err != nil {
		return fmt.Errorf("Could not insert item")
	}
	
	return nil
}

func getImg(c echo.Context) error {
	// Create image path
	imgPath := path.Join(ImgDir, c.Param("imageFilename"))

	if !strings.HasSuffix(imgPath, ".jpg") {
		res := Response{Message: "Image path does not end with .jpg"}
		return c.JSON(http.StatusBadRequest, res)
	}
	if _, err := os.Stat(imgPath); err != nil {
		c.Logger().Debugf("Image not found: %s", imgPath)
		imgPath = path.Join(ImgDir, "default.jpg")
	}
	return c.File(imgPath)
}

func getItemById(c echo.Context) error {
	receivedID := c.Param("id")
	receivedIDint, err := strconv.Atoi(receivedID)
	if err != nil {
		errorHandler(err, c, http.StatusInternalServerError, "Could not convert id to int")
	}

	items, err := readItemsFromDB()
	if err != nil {
		errorHandler(err, c, http.StatusInternalServerError, "Could not open file")
	}

	for _, item := range items {
		if item.ID == receivedIDint{
			return c.JSON(http.StatusOK, item)
		}
	}
	return c.JSON(http.StatusNotFound, Response{Message: "Item with that ID was not found"})
}

func getItemBySearch(c echo.Context) error{
	keyword := c.QueryParam("keyword")
	c.Logger().Debugf("keyword received: " + keyword)
	var searchList []Item

	db, err := sql.Open("sqlite3", DBPath)
	if err != nil {
		errorHandler(err, c, http.StatusInternalServerError, "Could not open file")
	}
	defer db.Close() 
	
	rows, err := db.Query(SelectAllQuery + " WHERE LOWER(items.name) LIKE '%" + strings.ToLower(keyword) + "%'")
	if err != nil{
		errorHandler(err, c, http.StatusInternalServerError, "Could not retrieve items with key word")
	}
	
	for rows.Next() {
		var item Item
	
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.ImageName); err != nil {
			errorHandler(err, c, http.StatusInternalServerError, "Could scan items")
		}
		c.Logger().Debugf("An item was picked up in the search")
		searchList = append(searchList, item)
	}

	if len(searchList) == 0{
		return c.JSON(http.StatusOK, Response{Message:"No such items found with the keyword " + keyword})
	}

	return c.JSON(http.StatusOK, Items{Items:searchList})
}

func imageHasher(image *multipart.FileHeader) (string, error) {
	imgFile, err := image.Open()
	if err != nil {
		return "", fmt.Errorf("Could not open image file")
	}
	defer imgFile.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, imgFile); err != nil {
		return "", fmt.Errorf("Could not carry out hash")
	}
	hashed := hasher.Sum(nil)
	hashString := hex.EncodeToString(hashed)

	hashString += ".jpg"
	return hashString, nil
}

func main() {
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Logger.SetLevel(log.DEBUG)

	frontURL := os.Getenv("FRONT_URL")
	if frontURL == "" {
		frontURL = "http://localhost:3000"
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{frontURL},
		AllowMethods: []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete},
	}))

	// Routes
	e.GET("/", root)
	e.POST("/items", addItem)
	e.GET("/items", getItems)
	e.GET("/image/:imageFilename", getImg)
	e.GET("/items/:id", getItemById)
	e.GET("/search", getItemBySearch)

	// Start server
	e.Logger.Fatal(e.Start(":9000"))
}
