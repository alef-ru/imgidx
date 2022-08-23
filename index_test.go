package imgidx_test

import (
	"github.com/alef-ru/imgidx"
	"github.com/alef-ru/imgidx/embedders"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"sync"
	"testing"
)

func createPockemonIndex(t *testing.T) imgidx.Index {
	imgDirPath := "./testdata/pokemon"
	files, err := os.ReadDir(imgDirPath)
	if err != nil {
		t.Fatalf("failed to read files in %s : %v", imgDirPath, err)
	}
	embedder := embedders.Composition([]embedders.ImageEmbedder{
		embedders.NewAspectRatioEmbedder(),
		embedders.NewColorDispersionEmbedder(),
		embedders.NewLowResolutionEmbedder(8, 8),
	})
	idx, err := imgidx.NewKDTreeImageIndex(embedder)
	if err != nil {
		t.Fatalf("failed to create index : %v", err)
	}
	for _, file := range files {
		_, err := imgidx.AddImageFile(idx, path.Join(imgDirPath, file.Name()), file.Name())
		if err != nil {
			t.Fatalf("failed to add image %s : %v", file.Name(), err)
		}
	}
	return idx
}

func loadImage(filePath string) (image.Image, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			panic(err)
		}
	}(f)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	img.ColorModel().Convert(color.RGBA{})
	return img, err
}

// Try to find image that is converted from PNG to JPEG and compressed
func TestIndexMatch(t *testing.T) {
	haystack := createPockemonIndex(t)
	needlePath := "testdata/compressed_abomasnow.jpg"
	expectedImg := "abomasnow.png"
	needle, err := loadImage(needlePath)
	if err != nil {
		t.Fatalf("failed to load image %v : %v", needlePath, err)
	}
	got, dist, err := haystack.Nearest(needle)
	if err != nil {
		t.Fatalf("Failed to find nearest image, : %v", err)
	}
	if got != expectedImg {
		t.Fatalf("Failed to find nearest image, got '%v', want '1:1 image'", got)
	}
	if dist > 0.025 { // Since it is the same image, the difference must be low.
		t.Fatalf("Distance between image is too far : %v", dist)
	}
}

// In this test I remove the matched image and try to search for the similar image.
// I don't care about the image found. The main point, is that the distance is big.
func TestIndexNotMatch(t *testing.T) {
	haystack := createPockemonIndex(t)
	needlePath := "testdata/compressed_abomasnow.jpg"
	expectedImg := "abomasnow.png"
	needle, err := loadImage(needlePath)
	if err != nil {
		t.Fatalf("failed to load image %v : %v", needlePath, err)
	}

	// Remove matched image to insure, that match is impossible
	cnt, err := haystack.Remove(func(vec embedders.Vector, attrs interface{}) bool { return attrs == expectedImg })
	if err != nil || cnt != 1 {
		t.Fatalf("Failed to remove image %v : %v", expectedImg, err)
	}
	_, dist, err := haystack.Nearest(needle)
	if err != nil {
		t.Fatalf("Failed to find nearest image, : %v", err)
	}
	if dist < 3 { // Since we removed the matched image, the difference must be quite high
		t.Fatalf("Distance between image is too close : %v", dist)
	}
}

// In this test we try to find match for the image that differs form original significantly
// (aspect ratio, colors, format etc.).
func TestIndexWeekMatch(t *testing.T) {
	haystack := createPockemonIndex(t)
	needlePath := "testdata/distorted_abomasnow.jpg"
	expectedImg := "abomasnow.png"
	needle, err := loadImage(needlePath)
	if err != nil {
		t.Fatalf("failed to load image %v : %v", needlePath, err)
	}

	// I don't care about the distance.
	// The main point, is that the propper image found
	got, _, err := haystack.Nearest(needle)
	if err != nil {
		t.Fatalf("Failed to find nearest image, : %v", err)
	}
	if got != expectedImg {
		t.Fatalf("Failed to find nearest image, got '%v', want '1:1 image'", got)
	}

}

func generateTestImages(t *testing.T) imgidx.Index {
	e := embedders.NewAspectRatioEmbedder()
	idx, err := imgidx.NewKDTreeImageIndex(e)
	if err != nil {
		t.Fatalf("Failed to create idx, : %v", err)
	}
	if idx == nil {
		t.Fatalf("Failed to create idx, NewKDTreeImageIndex() returned nil, nil")
	}

	seed := map[string]image.Image{
		"1:1 image":                 image.NewRGBA(image.Rect(0, 0, 100, 100)),
		"almost 1:1 vertical image": image.NewRGBA(image.Rect(0, 0, 99, 101)),
		"2:1 image":                 image.NewRGBA(image.Rect(0, 0, 200, 100)),
		"1:2 image":                 image.NewRGBA(image.Rect(0, 0, 100, 200)),
	}
	for name, value := range seed {
		_, err := idx.AddImage(value, name)
		if err != nil {
			t.Fatalf("Failed to add vector '%v' to idx, : %v", name, err)
		}
	}
	return idx
}

func TestIndexRemove(t *testing.T) {
	needle := image.NewRGBA(image.Rect(0, 0, 101, 99))

	tests := []struct {
		name           string
		f              func(vec embedders.Vector, attrs interface{}) bool
		want           int
		nearestImgWant string
		wantErr        bool
	}{
		{
			"delete nothing",
			func(vec embedders.Vector, attrs interface{}) bool { return false },
			0,
			"1:1 image",
			false,
		}, {
			"delete square by vec",
			func(vec embedders.Vector, attrs interface{}) bool {
				return vec[0] == 0
			},
			1,
			"almost 1:1 vertical image",
			false,
		}, {
			"delete square by attrs",
			func(vec embedders.Vector, attrs interface{}) bool { return attrs == "1:1 image" },
			1,
			"almost 1:1 vertical image",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := generateTestImages(t)

			got, err := idx.Remove(tt.f)
			if (err != nil) != tt.wantErr {
				t.Fatalf("kDTreeIndex.Remove() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("kDTreeIndex.Remove() = %v, want %v", got, tt.want)
			}
			nearestImgGot, _, err := idx.Nearest(needle)
			if err != nil {
				t.Fatalf("Failed to find nearest image, : %v", err)
			}
			if tt.nearestImgWant != nearestImgGot {
				t.Fatalf("Failed to find nearest image, got '%v', want '%v'", nearestImgGot, tt.nearestImgWant)
			}
		})
	}
}

func TestIndexConcurrentWrite(t *testing.T) {
	const iterations = 1000
	deletionResults := make(chan int, iterations)
	extraImage := image.NewRGBA(image.Rect(0, 0, 100, 100))
	removeExtraImages := func(vec embedders.Vector, attrs interface{}) bool {
		return attrs == "extra"
	}
	idx := createPockemonIndex(t)
	originalIdxLen := idx.GetCount()
	var wg sync.WaitGroup
	wg.Add(iterations * 2)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			_, err := idx.AddImage(extraImage, "extra")
			if err != nil {
				panic("Failed to add extra image to index")
			}
		}()
		go func() {
			defer wg.Done()
			deleted, err := idx.Remove(removeExtraImages)
			if err != nil {
				panic("Failed to remove extra images from index")
			}
			deletionResults <- deleted
		}()
	}
	wg.Wait()
	close(deletionResults)
	deletedTotal, err := idx.Remove(removeExtraImages)
	if err != nil {
		t.Fatalf("Failed to remove extra images from index, : %v", err)
	}
	for deleted := range deletionResults {
		deletedTotal += deleted
	}
	if deletedTotal != iterations {
		t.Fatalf("%v images was expected to be removed, %v was removed in fact", iterations, deletedTotal)
	}
	if idx.GetCount() != originalIdxLen {
		t.Fatalf("%v images was expected to be in index, %v in fact", originalIdxLen, idx.GetCount())
	}
}

func TestAddImageUrl(t *testing.T) {
	server := runTestImgHttpServer()
	defer server.Close()

	imgUrl := server.URL + "/abra.png"
	imgPath := "./testdata/pokemon/abra.png"
	idx := createPockemonIndex(t)
	urlVec, err := imgidx.AddImageUrl(idx, imgUrl, "form url")
	if err != nil {
		t.Fatalf("Failed to add image from url %v : %v", imgUrl, err)
	}

	fileVec, err := imgidx.AddImageFile(idx, imgPath, "form file")
	if err != nil {
		t.Fatalf("Failed to add image from file %v : %v", imgPath, err)
	}

	if !reflect.DeepEqual(urlVec, fileVec) {
		t.Fatalf("Vectors from url and file are not equal")
	}
}

func runTestImgHttpServer() *httptest.Server {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			imgPath := path.Join("testdata/pokemon/", r.URL.Path[1:])
			http.ServeFile(w, r, imgPath)
		}))
	return server
}
