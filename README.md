# HayekPool

High performance SHA256 mining stratum with Web-interface written in Golang for hayekchain.

**Stratum feature list:**

* Be your own pool
* Rigs availability monitoring
* Keep track of accepts, rejects, blocks stats
* Easy detection of sick rigs
* Daemon failover list
* Concurrent shares processing
* Beautiful Web-interface

![](screenshot.png)

## Installation

Dependencies:

  * go-1.14
  * hayek node
  * redis-server >= 2.8.0
  * nodejs 4.2.6 & npm 3.5.2
  * nginx

Build stratum:

```
$ go build
```

### Building Frontend

Install nodejs. I suggest using LTS version >= 4.x from https://github.com/nodesource/distributions or from your Linux distribution or simply install nodejs on Ubuntu Xenial 16.04.

The frontend is a single-page Ember.js application that polls the pool API to render miner stats.

    cd www

Change <code>ApiUrl: '//example.net/'</code> in <code>www/config/environment.js</code> to match your domain name. Also don't forget to adjust other options.

    npm install -g ember-cli@2.9.1
    npm install -g bower
    npm install
    bower install
    ./build.sh

Configure nginx to serve API on <code>/api</code> subdirectory.
Configure nginx to serve <code>www/dist</code> as static website.

#### Serving API using nginx

Create an upstream for API:

    upstream api {
        server 127.0.0.1:8080;
    }

and add this setting after <code>location /</code>:

    location /api {
        proxy_pass http://api;
    }

#### Customization

You can customize the layout using built-in web server with live reload:

    ember server --port 8082 --environment development

**Don't use built-in web server in production**.

Check out <code>www/app/templates</code> directory and edit these templates
in order to customise the frontend.


### Running Stratum

    ./HayekPool hayekconfig.json

If you need to bind to privileged ports and don't want to run from `root`:

    sudo apt-get install libcap2-bin
    sudo setcap 'cap_net_bind_service=+ep' /path/to/HayekPool

## Configuration

Configuration is self-describing, just copy *config.example.json* to *config.json* and run stratum with path to config file as 1st argument.

```javascript
{
	"coin": "HYK",
	"pool": {
		"address": "YOU_HYK_ADDRESS",
		"bypassAddressValidation": true,
		"bypassShareValidation": true,
		
		"threads": 2,
		"name": "main",
		
		"estimationWindow": "15m",
		"luckWindow": "24h",
		"largeLuckWindow": "72h",
		
		"blockRefreshInterval": "1s",
		"hashrateExpiration": "3h",
		"stateUpdateInterval": "3s",
		
		"newrelicEnabled": false,
		"newrelicName": "MyStratum",
		"newrelicKey": "SECRET_KEY",
		"newrelicVerbose": false,
		
		"stratum": {
			"timeout": "15m",
		
			"listen": [
				{
					"host": "0.0.0.0",
					"port": 1111,
					"diff": 2000,
					"maxConn": 32768
				},
				{
					"host": "0.0.0.0",
					"port": 3333,
					"diff": 8000,
					"maxConn": 32768
				},
				{
					"host": "0.0.0.0",
					"port": 5555,
					"diff": 12000,
					"maxConn": 32768
				}
			]
		},
		
		"upstreamCheckInterval": "5s",
		
		"upstream": [
			{
				"name": "main",
				"host": "127.0.0.1",
				"port": 8585,
				"timeout": "10s"
			}
		]
	},

    "policy": {
        "workers": 8,
        "resetInterval": "60m",
        "refreshInterval": "1m",

        "banning": {
            "enabled": false,
            "ipset": "blacklist",
            "timeout": 1800,
            "invalidPercent": 30,
            "checkThreshold": 30,
            "malformedLimit": 5
        },
        "limits": {
            "enabled": false,
            "limit": 30,
            "grace": "5m",
            "limitJump": 10
        }
    },

	"api": {
		"enabled": true,
		"purgeOnly": false,
		"purgeInterval": "10m",
		"listen": "0.0.0.0:8080",
		"statsCollectInterval": "5s",
		"hashrateWindow": "30m",
		"hashrateLargeWindow": "3h",
		"luckWindow": [64, 128, 256],
		"payments": 30,
		"blocks": 50
	},

	"redis": {
		"endpoint": "127.0.0.1:6379",
		"poolSize": 10,
		"database": 0,
		"password": ""
	},

	"unlocker": {
		"enabled": true,
		"poolFee": 1.0,
		"poolFeeAddress": "0x30bd37b278969258e47b686a266a61b994d1da8c",
		"donate": true,
		"depth": 120,
		"immatureDepth": 20,
		"keepTxFees": false,
		"interval": "10m",
		"daemon": "http://127.0.0.1:8585",
		"timeout": "10s"
	},

	"payouts": {
		"enabled": true,
		"requirePeers": 3,
		"interval": "120m",
		"daemon": "http://127.0.0.1:8585",
		"timeout": "10s",
		"address": "",
		"gas": "21000",
		"gasPrice": "50000000000",
		"autoGas": true,
		"threshold": 500000000,
		"bgsave": false
	}
}
```
