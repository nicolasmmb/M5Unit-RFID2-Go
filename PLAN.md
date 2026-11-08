# PLAN — M5Unit-RFID2-Go

Driver Go do **Unit RFID2** (SKU U031-B): WS1850S (registradores MFRC522),
13,56 MHz, I2C em 0x28. 100% Go.

- Módulo: `github.com/nicolasmmb/M5Unit-RFID2-Go`
- Estado: v0.1.0 implementada. Falta validação em hardware.

## 1. Hardware e protocolo

| Item | Valor |
|---|---|
| Chip | WS1850S (compatível com MFRC522) |
| Endereço | 0x28 (I2C) |
| Protocolo | write `[reg, valor]`; read `[reg]` + N bytes |
| Norma | ISO/IEC 14443 A/B |
| Cartões | MIFARE Classic, NTAG |

O C oficial (`MFRC522_I2C.cpp`) foi a referência para: registradores, sequência
de init (TMode 0x8D, TPrescaler 0x3E, TReload 30, TxASK 0x40, Mode 0x3D,
antena nos bits 0 e 1 do TxControl), comando MFAuthent com máscara de erro 0x1B,
e os comandos de bloco 0x30/0xA0.

## 2. Escopo da v0.1.0

Dentro: init/versão/antena, REQA + anticolisão + select (UID/SAK/ATQA), auth
A/B, leitura e escrita de bloco, halt, CRC_A em software, testes de host,
benchmarks, exemplo, CI com gate de agnosticismo.

Fora: cascata 2/3 (UID de 7/10 bytes), ISO14443-B, comandos de valor do MIFARE,
Ultralight, e o modo de descoberta contínua.

## 3. Decisões

1. **CRC_A em software** (0x8408 refletido, init 0x6363). Motivo: determinístico
   e testável no host; o vetor real `50 00 57 CD` (HALT) está no teste.
2. **Crypto1 no chip.** O `MFAuthent` faz tudo; nada de criptografia em Go.
3. **Buffer separado para o FIFO.** O frame é montado em `d.tx`; escrever o
   FIFO com o mesmo buffer sobrescrevia o primeiro byte do frame — bug pego
   pelos testes e documentado no README.
4. **`readFIFO` lê `FIFOLevel`** antes de ler, para não devolver lixo curto.

## 4. Testes

Emulador de chip com registradores, FIFO e cartão roteirizado:

- init, versão errada, chip morto, barramento nulo;
- seleção completa (3 frames), colisão, BCC errado, sem cartão (timeout);
- auth ok e falha (máscara 0x1B);
- read com CRC correto e corrompido;
- write com ACK 0x0A e ACK errado;
- halt (frame `50 00 57 CD`);
- CRC_A contra implementação independente e o vetor do HALT;
- 0 alocação e footprint 104 B.

## 5. Fases

- [x] Pesquisa (doc, C oficial, registradores).
- [x] Núcleo, CRC, select, auth, read, write, halt.
- [x] Testes, contratos, benchmarks.
- [x] README, BENCHMARKS, Makefile, CI, exemplo.
- [ ] Validação em hardware com um cartão.
- [ ] Publicar e tag `v0.1.0`.

## 6. Pendências

- [ ] Flash do `examples/read` com um cartão e conferir UID/bloco.
- [ ] Cascata 2/3 para UID de 7 bytes (NTAG21x).
- [ ] Medir tempos reais de select/read.
- [ ] ISO14443-B se algum projeto precisar.

## 7. Referências

- https://docs.m5stack.com/en/unit/Unit_RFID2
- `M5-ProductExampleCodes/Unit/RFID/Arduino/RFID/MFRC522_I2C.cpp`
- Datasheets WS1850S e MFRC522 (NXP)
- ISO/IEC 14443-3
