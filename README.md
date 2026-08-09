# 📖 Bilingua — traductor de libros PDF

Traduce libros en PDF (inglés → español y otros idiomas) dejando el original y la
traducción lado a lado, o solo la traducción. **Un solo archivo `.exe`**, sin
instalación ni dependencias. Motor: **DeepL** (traducción de alta calidad).

## Para quien lo usa (Windows)

1. Doble clic en **`Bilingua.exe`**. Se abre solo en el navegador.
2. Pega tu **clave de DeepL** (gratis en <https://www.deepl.com/pro-api>, 500.000
   caracteres al mes sin costo). Se guarda en tu navegador; se pide una sola vez.
3. Elige idioma y formato (bilingüe o solo traducción).
4. Arrastra el PDF y pulsa **Traducir**. Verás el progreso en vivo.
5. Al terminar, **Descargar PDF traducido**.

> El PDF debe tener texto. Si es un escaneo (solo imágenes), primero hay que pasarle OCR.

## Modo consola (opcional)

```
Bilingua.exe --cli --in libro.pdf --key TU_CLAVE --source EN --target ES --mode bilingual
```

## Compilar (para quien distribuye)

Requiere [Go](https://go.dev/dl/). Sin CGo, así que compila el `.exe` de Windows
desde Mac/Linux sin herramientas extra:

```
./build.sh          # genera dist/Bilingua.exe, Bilingua-mac, Bilingua-linux
```

## Licencia
MIT.
