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

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	_ "github.com/mattn/go-sqlite3"
)

type Item struct {
	ID			int `json:"id"`
	Name 		string `json:"name"`
	Category 	string `json:"category"`
	ImageName 	string `json:"image"`
}

type Items struct {
	Items []Item `json:"items"`
}

const (
	ImgDir = "images"
	ItemsJson = "app/items.json"
	DBPath = "../db/mercari.sqlite3"
)

type Response struct {
	Message string `json:"message"`
}

type ResponseWithID struct {
	Message string 	`json:"message"`
	IDList	[]int 	`json:"idList"`
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
		return nil, err
	}
	defer db.Close()
	
	rows, err := db.Query("SELECT id, name, category, image_name FROM items")
	if err != nil{
		return nil, err
	}

	var items []Item

	for rows.Next() {
		var item Item
	
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.ImageName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

func getItems(c echo.Context) error {
	items, err := readItemsFromDB()
	if err != nil {
		return err
	}	
	return c.JSON(http.StatusOK, Items{Items:items})
}

func addItem(c echo.Context) error {
	// Get form data
	name := c.FormValue("name")
	category := c.FormValue("category")
	idstring := c.FormValue("id")
	if idstring == "" {
		return c.JSON(http.StatusBadRequest, Response{Message: "Please include an id"})
	}
	id, err := strconv.Atoi(idstring)
	if err != nil {
		return c.JSON(http.StatusBadRequest, Response{Message: "Please include an integer id"})
	}

	// Making sure no two same IDs are registered
	currentItems, err := readItemsFromDB()
	if err != nil {
		c.Logger().Debugf("Items couldn't be read")
		return errorHandler(err, c, http.StatusInternalServerError, "Could not retrieve items")
	}

	var idList []int
	for _, item := range currentItems {
		idList = append(idList, item.ID)
	}

	for _, idnumber := range idList {
		if idnumber == id {
			res := ResponseWithID{Message:"That ID already exists", IDList: idList}
			return c.JSON(http.StatusBadRequest, res)
		} 
	} 

	image, err := c.FormFile("image")
	if err != nil {
		return errorHandler(err, c, http.StatusBadRequest, "Image not found")
	}

	c.Logger().Infof("Received item. Name: %s, Category: %s, ID: %d", name, category, id)

	// hash img
	imgFile, err := image.Open()
	if err != nil {
		return errorHandler(err, c, http.StatusBadRequest, "Image not found")
	}
	defer imgFile.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, imgFile); err != nil {
		return errorHandler(err, c, http.StatusInternalServerError, "Couldn't hash")
	}
	hashed := hasher.Sum(nil)
	hashString := hex.EncodeToString(hashed)

	hashString += ".jpg"

	if err = addItemtoDB(name, category, hashString, id); err != nil {
		return errorHandler(err, c, http.StatusInternalServerError, "Could not add items")
	}

	message := fmt.Sprintf("item received: %s, %s, ID number %d", name, category, id)
	res := Response{Message: message}

	return c.JSON(http.StatusOK, res)
}

func addItemtoDB(name string, category string, image string, id int) error {
	db, err := sql.Open("sqlite3", DBPath)
	if err != nil {
		return err
	}
	defer db.Close() 

	_, err = db.Exec("INSERT INTO items (id, name, category, image_name) VALUES (?,?,?,?)", id, name, category, image)
	if err != nil {
		return err
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
	
	rows, err := db.Query("SELECT * FROM items WHERE LOWER(name) LIKE '%" + keyword + "%'")
	if err != nil{
		return err
	}
	
	for rows.Next() {
		var item Item
	
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.ImageName); err != nil {
			return err
		}
		c.Logger().Debugf("An item was picked up in the search")
		searchList = append(searchList, item)
	}

	if len(searchList) == 0{
		return c.JSON(http.StatusOK, Response{Message:"No such items found with the keyword " + keyword})
	}

	return c.JSON(http.StatusOK, Items{Items:searchList})
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
