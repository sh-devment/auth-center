package main

var regionRU = regionDef{
	Lang: "ru",

	Text: pageText{
		Title:      "auth-center",
		SignInWith: "Войти через:",

		TileGoogle:   "google",
		TileYandex:   "яндекс",
		TileTelegram: "tg",
		TileSolana:   "solana",

		OpenInTelegram: "открыть в telegram",
		Refresh:        "обновить",

		ConnectWallet: "подключить кошелёк",
		NoWallet:      "кошелёк не найден —",
		OpenInPhantom: "открыть в phantom",

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
