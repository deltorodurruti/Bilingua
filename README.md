# 📖 Bilingua

Traductor de libros en **PDF** (inglés → español y otros idiomas). Deja **solo la traducción** o el original y la traducción juntos. Funciona también con **PDFs escaneados** (los lee con OCR) y **copia las figuras** del original junto a su leyenda.

Un solo archivo, sin instalar nada: se abre en tu navegador o se usa desde la terminal.

---

## ⬇ Descargar

### Windows
👉 **[Descargar Bilingua para Windows](https://github.com/deltorodurruti/Bilingua/releases/latest)**

Descomprime el ZIP (clic derecho → **Extraer todo**) y haz doble clic en `Bilingua.exe`.

### Mac
👉 **[Descargar Bilingua para Mac](https://github.com/deltorodurruti/Bilingua/releases/latest)** (`Bilingua-mac`, sirve para Intel y Apple Silicon)

La primera vez, macOS bloquea los programas descargados de internet. Para desbloquearlo, abre la app **Terminal** y pega esto:

```bash
cd ~/Downloads
xattr -c Bilingua-mac 2>/dev/null
chmod +x Bilingua-mac
```

(Si dice *«No such xattr»*, ya estaba desbloqueado: sigue adelante.)

Luego ya puedes usarlo (ver abajo).

---

## 🖱 Uso con ventana (lo más simple)

Doble clic en el programa, o desde la terminal:

```bash
./Bilingua-mac
```

Se abre tu navegador con la interfaz. Pega tu clave de DeepL la primera vez, arrastra el PDF y pulsa **Traducir**. Al terminar, descarga el PDF traducido.

---

## ⌨️ Uso en la terminal (Mac)

```bash
./Bilingua-mac --in libro.pdf
```

Deja el resultado al lado del original, como `libro (traducido).pdf`.

**La primera vez** pasa tu clave; queda guardada y no hay que repetirla:

```bash
./Bilingua-mac --key TU_CLAVE_DEEPL --in libro.pdf
```

### Ejemplos

```bash
# ver el avance paso a paso (útil en libros largos)
./Bilingua-mac --in libro.pdf --verbose

# original y traducción juntos
./Bilingua-mac --in libro.pdf --mode bilingual

# elegir dónde guardar
./Bilingua-mac --in libro.pdf --out ~/Desktop/traducido.pdf

# traducir a otro idioma
./Bilingua-mac --in libro.pdf --target EN-US

# ver todas las opciones
./Bilingua-mac --help
```

### Opciones

| Opción | Para qué sirve |
|---|---|
| `--in` | el PDF a traducir |
| `--out` | dónde guardar (por defecto, junto al original) |
| `--mode` | `translation` (solo traducción, por defecto) · `bilingual` (con el original) |
| `--target` | idioma de destino: `ES`, `EN-US`, `EN-GB`, `FR`, `DE`, `IT`, `PT-BR`… |
| `--source` | idioma de origen; si lo omites, se detecta solo |
| `--verbose` | muestra cada paso con la hora y los segundos transcurridos |
| `--quiet` | no muestra nada salvo errores |
| `--key` | tu clave de DeepL (solo la primera vez) |
| `--port` | puerto de la interfaz web (por defecto 8799) |

**Consejo:** para no escribir `./Bilingua-mac` cada vez, muévelo a una carpeta del sistema y llámalo por su nombre:

```bash
sudo mkdir -p /usr/local/bin
sudo mv ~/Downloads/Bilingua-mac /usr/local/bin/bilingua
bilingua --in libro.pdf
```

---

## 🔑 La clave de DeepL

### Conseguirla (gratis, una vez)

1. Entra a **[deepl.com/pro-api](https://www.deepl.com/pro-api)** y pulsa **Sign up for free** → elige el plan **DeepL API Free** (500.000 caracteres al mes, sin costo).
2. Crea la cuenta con tu correo. **Te pedirá una tarjeta para verificar tu identidad; en el plan Free no se cobra nada** (solo si algún día pasas a Pro).
3. Confirma el correo e inicia sesión.
4. Ve a **[deepl.com/your-account/keys](https://www.deepl.com/en/your-account/keys)** (o arriba a la derecha: tu cuenta → pestaña **API keys**).
5. Copia la clave. Es una línea larga que **termina en `:fx`**, así:
   `1a2b3c4d-5e6f-7890-abcd-ef1234567890:fx`

### Guardarla

```bash
bilingua --key 1a2b3c4d-5e6f-7890-abcd-ef1234567890:fx --in libro.pdf
```

Con eso queda guardada y **no la vuelves a escribir nunca**.

Se guarda **en tu equipo** (`~/Library/Application Support/Bilingua/deepl_key.txt` en Mac, con permisos que solo te dejan leerla a ti) y no se vuelve a pedir. Nunca se muestra en pantalla ni sale del computador salvo hacia DeepL.

Si prefieres que **no quede en el historial de la terminal**, usa la variable de entorno en lugar de `--key`:

```bash
export BILINGUA_DEEPL_KEY="tu-clave:fx"
bilingua --in libro.pdf
```

### Varias claves (más cuota)

Cada cuenta gratuita da 500.000 caracteres al mes. Puedes tener **varias claves**: cuando una se agota, Bilingua **sigue con la siguiente sin perder lo ya traducido**.

```bash
bilingua --key PRIMERA:fx --in libro.pdf    # guarda la primera
bilingua --key SEGUNDA:fx --in libro.pdf    # AÑADE la segunda (no la reemplaza)
```

Se guardan una por línea en `~/Library/Application Support/Bilingua/deepl_key.txt`; puedes abrir ese archivo y editarlo o borrar la que ya no sirva. Al empezar, el programa te dice cuánta cuota te queda **sumando todas**.

---

## 📄 Libros grandes y escaneados

DeepL limita el tamaño de cada documento que se le envía: **10 MB** con la cuenta gratuita y **100 MB** con la de pago. Bilingua se encarga solo:

- Si el PDF **cabe**, lo manda entero.
- Si **no cabe**, lo divide en las **menos partes posibles**, traduce cada una y las **vuelve a unir** en un solo PDF. Mide cada parte de verdad antes de enviarla, así que una lámina pesada no rompe el proceso a mitad de camino.
- Si una parte falla, **las ya traducidas se conservan**: vuelve a ejecutar el mismo comando y se reutilizan en vez de gastar cuota otra vez. Lo mismo con los libros de texto: si se corta a mitad, retoma en el párrafo donde quedó.
- Al final comprueba que el PDF unido tenga **todas las páginas** del original.

> ⚠️ **Ojo con la cuota:** DeepL cobra un **mínimo de 50.000 caracteres por cada documento** enviado. Un libro dividido en 7 partes consume al menos 350.000 caracteres de tu cuota mensual, aunque tenga poco texto. Bilingua te avisa cuántas partes hará y cuánto costará antes de empezar.

Los libros **híbridos** (páginas escaneadas mezcladas con páginas de texto) se detectan y se mandan enteros al OCR, para que no se pierda ninguna página. Si alguna página no se puede leer, te avisa y toma esa misma ruta en vez de saltársela.

---

## 🛠 Para desarrolladores

Go 1.26+, sin dependencias nativas.

```bash
git clone https://github.com/deltorodurruti/Bilingua.git
cd Bilingua
go build .          # genera ./bilingua
go test ./...       # pruebas
./build.sh          # binarios para Windows, Mac (universal) y Linux en dist/
```

`build.sh` produce:

| Archivo | Para |
|---|---|
| `Bilingua.exe` | Windows, doble clic (sin ventana negra) |
| `bilingua-cli.exe` | Windows, terminal |
| `Bilingua-mac` | Mac universal (Intel + Apple Silicon) |
| `Bilingua-linux` | Linux |

Motor de traducción: [DeepL API](https://www.deepl.com/pro-api). La interfaz web se sirve en `127.0.0.1`: nada sale de tu equipo salvo el texto que se envía a DeepL para traducir.

## Licencia
MIT.
