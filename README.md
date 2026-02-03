# video-proxy

Сервис для пересылки видео-потока посредством udp сокета.

## Источник видео
Для запуска стриминга видео можно использовать ffmpeg.
```bash
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 \
-vcodec libx264 -preset ultrafast -tune zerolatency \
-x264-params "nal-hrd=cbr:force-cfr=1" -g 30 \
-payload_type 96 -f rtp udp://127.0.0.1:5000
```

## Потребитель видео
Получать видеопоток можно на порту 5001.
Можно в vlc открыть stream.sdp. 
Можно использовать встроенный в ffmpeg плеер.
```bash
ffplay -protocol_whitelist file,rtp,udp -i stream.sdp
```