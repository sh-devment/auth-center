package main

var regionRU = regionDef{
	Lang:     "ru",
	Telegram: false,

	Text: pageText{
		Title:      "auth-center",
		SignInWith: "Войти через:",

		TileGoogle:   "google",
		TileTelegram: "tg",
		TileSolana:   "solana",

		OpenInTelegram: "открыть в telegram",
		Refresh:        "обновить",

		ConnectWallet: "подключить кошелёк",
		NoWallet:      "кошелёк не найден —",
		OpenInPhantom: "открыть в phantom",

		Unavailable: "К сожалению, эта функция недоступна в вашем регионе",
		OK:          "ок",

		JS: jsText{
			ConnectWallet:    "подключить кошелёк",
			Connecting:       "подключение...",
			LookingForWallet: "ищем кошелёк...",
			OpeningWallet:    "открываем кошелёк...",
			Signing:          "подпись...",
			Connected:        "подключено",
		},
	},
}
