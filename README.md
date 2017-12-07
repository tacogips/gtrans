# JP translator

## install
```
go get github.com/tacogips/ej
```

## usage

get google translate api key from gcp developer console,
and set it into environment variable named `EJ_API_KEY`.

https://cloud.google.com/translate/docs/getting-started

```
export EJ_API_KEY="your_api_key"
```

and ej command with original sentence

```
> ej i am a man
i am a man
私は男です

> ej 我是一个男人
我是一个男人
私は男です

# translate to english if input word detected as japanese
> ej どすこい
Sumo exclamation
```

# Disclaimer
This tool uses google translation api that cant use free.
Heavy use leads you to bankruptcy.
At your own risk and wallet.
https://cloud.google.com/translate/pricing
