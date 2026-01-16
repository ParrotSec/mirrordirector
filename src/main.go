package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"
	"github.com/parrotsec/mirrordirector/server"
)

func initGeoIP() {
	/*licenseKey := os.Getenv("MAXMIND_LICENSE_KEY")
	accountID := os.Getenv("MAXMIND_ACCOUNT_ID")
	if licenseKey == "" || accountID == "" {
		fmt.Println("MAXMIND_LICENSE_KEY or MAXMIND_ACCOUNT_ID not set, skipping GeoIP update")
		return
	}

	confPath := "/etc/GeoIP.conf"
	content, err := os.ReadFile(confPath)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", confPath, err)
		return
	}

	newContent := strings.Replace(string(content), "YOUR_LICENSE_KEY_HERE", licenseKey, 1)
	newContent = strings.Replace(newContent, "YOUR_ACCOUNT_ID", accountID, 1)
	err = os.WriteFile(confPath, []byte(newContent), 0644)
	if err != nil {
		fmt.Printf("Error writing %s: %v\n", confPath, err)
		return
	}
	*/
	fmt.Println("Running geoipupdate...")
	cmd := exec.Command("wget https://deb.parrot.sh/direct/parrot/misc/.geoip/GeoLite2-City.mmdb -o /var/lib/GeoIP/GeoLite2-City.mmdb")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error downloading GeoIP database: %v\n", err)
	}
}

func main() {
	initGeoIP()
	time.Now().UTC().UnixNano()
	config := "https://deb.parrot.sh/direct/parrot/misc/director/config.yaml"
	cache := "/app/data/cache.db"
	Root := mirrors.Init(config)
	Root.Show()

	Fileset := files.Init(Root, cache)

	go func(F *files.Fileset, R *mirrors.Root, config string) {
		for {
			F.UpdateConfig(config, R)
			F.ScanMaster(*R)
			F.SaveCache(cache)
			R.Scan()
			R.Show()
			time.Sleep(time.Minute * 20)
		}
	}(&Fileset, &Root, config)

	fmt.Println("starting server")
	server.Serve(":8080", &Fileset, &Root)
}
