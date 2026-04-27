/**
 * zenith_link.c — implementation of the Zenith-Link binary telemetry protocol.
 *
 * Targets ARM Cortex-M4 / bare-metal C99.
 * No dynamic allocation. All buffers are caller-supplied.
 *
 * SHA-256 implemented from NIST FIPS 180-4.
 * Constant-time HMAC comparison to prevent timing attacks.
 */

#include "zenith_link.h"

#include <math.h>    /* fabsf */
#include <string.h>  /* memcpy, memset */

/* ── SHA-256 ─────────────────────────────────────────────────────────────── */

static const uint32_t _k[64] = {
    0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2,
};

#define ROTR32(x,n) (((x)>>(n))|((x)<<(32-(n))))
#define CH(x,y,z)   (((x)&(y))^(~(x)&(z)))
#define MAJ(x,y,z)  (((x)&(y))^((x)&(z))^((y)&(z)))
#define SIG0(x)     (ROTR32(x,2)^ROTR32(x,13)^ROTR32(x,22))
#define SIG1(x)     (ROTR32(x,6)^ROTR32(x,11)^ROTR32(x,25))
#define sig0(x)     (ROTR32(x,7)^ROTR32(x,18)^((x)>>3))
#define sig1(x)     (ROTR32(x,17)^ROTR32(x,19)^((x)>>10))

static void _sha256_block(uint32_t h[8], const uint8_t blk[64])
{
    uint32_t w[64];
    for (int i = 0; i < 16; i++) {
        w[i] = ((uint32_t)blk[4*i]<<24)|((uint32_t)blk[4*i+1]<<16)|
               ((uint32_t)blk[4*i+2]<<8)|(uint32_t)blk[4*i+3];
    }
    for (int i = 16; i < 64; i++)
        w[i] = sig1(w[i-2]) + w[i-7] + sig0(w[i-15]) + w[i-16];

    uint32_t a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];
    for (int i = 0; i < 64; i++) {
        uint32_t t1 = hh + SIG1(e) + CH(e,f,g) + _k[i] + w[i];
        uint32_t t2 = SIG0(a) + MAJ(a,b,c);
        hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d;
    h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;
}

void zl_sha256(const uint8_t *msg, size_t len, uint8_t digest[32])
{
    uint32_t h[8] = {
        0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,
        0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19,
    };

    uint8_t  blk[64];
    size_t   processed = 0;
    uint64_t bit_len   = (uint64_t)len * 8;

    while (processed + 64 <= len) {
        _sha256_block(h, msg + processed);
        processed += 64;
    }

    /* Final block(s) with padding */
    size_t rem = len - processed;
    memcpy(blk, msg + processed, rem);
    blk[rem++] = 0x80;

    if (rem > 56) {
        memset(blk + rem, 0, 64 - rem);
        _sha256_block(h, blk);
        rem = 0;
    }
    memset(blk + rem, 0, 56 - rem);
    /* Append bit length big-endian */
    for (int i = 7; i >= 0; i--)
        blk[56 + (7 - i)] = (uint8_t)(bit_len >> (i * 8));
    _sha256_block(h, blk);

    for (int i = 0; i < 8; i++) {
        digest[4*i]   = (uint8_t)(h[i]>>24);
        digest[4*i+1] = (uint8_t)(h[i]>>16);
        digest[4*i+2] = (uint8_t)(h[i]>>8);
        digest[4*i+3] = (uint8_t)(h[i]);
    }
}

void zl_hmac_sha256(
    const uint8_t *key, size_t key_len,
    const uint8_t *msg, size_t msg_len,
    uint8_t tag[32])
{
    uint8_t k_ipad[64], k_opad[64];
    uint8_t temp_key[32];

    /* If key longer than block, hash it first */
    if (key_len > 64) {
        zl_sha256(key, key_len, temp_key);
        key = temp_key;
        key_len = 32;
    }

    memset(k_ipad, 0x36, 64);
    memset(k_opad, 0x5c, 64);
    for (size_t i = 0; i < key_len; i++) {
        k_ipad[i] ^= key[i];
        k_opad[i] ^= key[i];
    }

    /* inner: SHA256(k_ipad || msg) */
    uint8_t inner[32];
    {
        /* We can't heap-allocate, so we use a two-block approach */
        uint32_t h[8] = {
            0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,
            0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19,
        };
        _sha256_block(h, k_ipad);

        /* Process message blocks */
        uint8_t blk[64];
        size_t processed = 0;
        uint64_t total = 64 + msg_len;

        while (processed + 64 <= msg_len) {
            _sha256_block(h, msg + processed);
            processed += 64;
        }

        size_t rem = msg_len - processed;
        memcpy(blk, msg + processed, rem);
        blk[rem++] = 0x80;
        if (rem > 56) {
            memset(blk + rem, 0, 64 - rem);
            _sha256_block(h, blk);
            rem = 0;
        }
        memset(blk + rem, 0, 56 - rem);
        uint64_t bits = total * 8;
        for (int i = 7; i >= 0; i--) blk[56 + (7-i)] = (uint8_t)(bits >> (i*8));
        _sha256_block(h, blk);

        for (int i = 0; i < 8; i++) {
            inner[4*i]   = (uint8_t)(h[i]>>24); inner[4*i+1] = (uint8_t)(h[i]>>16);
            inner[4*i+2] = (uint8_t)(h[i]>>8);  inner[4*i+3] = (uint8_t)(h[i]);
        }
    }

    /* outer: SHA256(k_opad || inner) */
    uint8_t outer_input[64 + 32];
    memcpy(outer_input,      k_opad, 64);
    memcpy(outer_input + 64, inner,  32);
    zl_sha256(outer_input, 96, tag);
}

/* ── Constant-time comparison ───────────────────────────────────────────── */

static bool _ct_eq(const uint8_t *a, const uint8_t *b, size_t n)
{
    uint8_t diff = 0;
    for (size_t i = 0; i < n; i++) diff |= a[i] ^ b[i];
    return diff == 0;
}

/* ── Delta detection ─────────────────────────────────────────────────────── */

bool zl_changed(float new_val, float old_val)
{
    if (old_val == 0.0f)
        return fabsf(new_val) > 1e-4f;
    float rel = fabsf((new_val - old_val) / old_val);
    return rel * ZL_DELTA_THRESHOLD_DEN > ZL_DELTA_THRESHOLD_NUM;
}

uint8_t zl_compute_presence(
    const zl_state_t *s,
    const zl_state_t *p,
    bool              saturated)
{
    uint8_t pres = 0;

    if (zl_changed(s->pitch,   p->pitch)   ||
        zl_changed(s->yaw,     p->yaw)     ||
        zl_changed(s->roll,    p->roll))        pres |= ZL_PRESENCE_ATTITUDE;

    if (zl_changed(s->omega_x, p->omega_x) ||
        zl_changed(s->omega_y, p->omega_y) ||
        zl_changed(s->omega_z, p->omega_z))     pres |= ZL_PRESENCE_ANGULAR_VEL;

    if (zl_changed((float)s->latitude,  (float)p->latitude)  ||
        zl_changed((float)s->longitude, (float)p->longitude) ||
        zl_changed((float)s->altitude,  (float)p->altitude))  pres |= ZL_PRESENCE_POSITION;

    if (zl_changed(s->battery_voltage, p->battery_voltage))   pres |= ZL_PRESENCE_BATTERY_VOLTAGE;
    if (zl_changed(s->rssi,            p->rssi))              pres |= ZL_PRESENCE_RSSI;

    if (!saturated) {
        if (zl_changed(s->solar_voltage, p->solar_voltage))   pres |= ZL_PRESENCE_SOLAR_VOLTAGE;
        if (zl_changed(s->chassis_temp,  p->chassis_temp))    pres |= ZL_PRESENCE_CHASSIS_TEMP;
        if (zl_changed(s->cpu_temp,      p->cpu_temp))        pres |= ZL_PRESENCE_CPU_TEMP;
    }
    return pres;
}

/* ── Encoder ─────────────────────────────────────────────────────────────── */

int zl_encode(
    uint8_t          *buf,
    size_t            buf_len,
    const zl_state_t *state,
    const zl_state_t *prev,
    uint32_t          seq,
    uint64_t          ts_us,
    bool              saturated,
    const uint8_t    *hmac_key,
    size_t            key_len)
{
    if (buf_len < ZL_MAX_FRAME_SIZE)
        return ZL_ERR_BUF_TOO_SMALL;

    bool is_full = (prev == NULL);
    uint8_t flags    = is_full ? ZL_FLAG_FULL_SYNC : 0;
    if (saturated) flags |= ZL_FLAG_CRIT_MODE;

    uint8_t presence = is_full ? ZL_PRESENCE_ALL
                               : zl_compute_presence(state, prev, saturated);

    /* Header */
    size_t o = 0;
    zl_w16(buf + o, ZL_MAGIC);          o += 2;
    buf[o++] = ZL_VERSION;
    zl_w32(buf + o, seq);               o += 4;
    zl_w64(buf + o, ts_us);             o += 8;
    buf[o++] = flags;
    /* Presence byte */
    buf[o++] = presence;

    /* Payload */
#define W16S(v)  do { int16_t _v = (int16_t)(v); zl_w16(buf+o,(uint16_t)_v); o+=2; } while(0)
#define W16U(v)  do { zl_w16(buf+o,(uint16_t)(v)); o+=2; } while(0)
#define W32S(v)  do { int32_t _v = (int32_t)(v); zl_w32(buf+o,(uint32_t)_v); o+=4; } while(0)

    if (presence & ZL_PRESENCE_ATTITUDE) {
        W16S((int32_t)(state->pitch   * ZL_SCALE_ANGLE));
        W16S((int32_t)(state->yaw     * ZL_SCALE_ANGLE));
        W16S((int32_t)(state->roll    * ZL_SCALE_ANGLE));
    }
    if (presence & ZL_PRESENCE_ANGULAR_VEL) {
        W16S((int32_t)(state->omega_x * ZL_SCALE_ANG_VEL));
        W16S((int32_t)(state->omega_y * ZL_SCALE_ANG_VEL));
        W16S((int32_t)(state->omega_z * ZL_SCALE_ANG_VEL));
    }
    if (presence & ZL_PRESENCE_POSITION) {
        W32S((int64_t)(state->latitude  * ZL_SCALE_LAT_LON));
        W32S((int64_t)(state->longitude * ZL_SCALE_LAT_LON));
        W32S((int64_t)(state->altitude  * ZL_SCALE_ALT));
    }
    if (presence & ZL_PRESENCE_SOLAR_VOLTAGE)   W16U((uint32_t)(state->solar_voltage   * ZL_SCALE_VOLTAGE));
    if (presence & ZL_PRESENCE_BATTERY_VOLTAGE) W16U((uint32_t)(state->battery_voltage * ZL_SCALE_VOLTAGE));
    if (presence & ZL_PRESENCE_CHASSIS_TEMP)    W16S((int32_t)(state->chassis_temp    * ZL_SCALE_TEMP));
    if (presence & ZL_PRESENCE_CPU_TEMP)        W16S((int32_t)(state->cpu_temp        * ZL_SCALE_TEMP));
    if (presence & ZL_PRESENCE_RSSI)            W16S((int32_t)(state->rssi            * ZL_SCALE_RSSI));

#undef W16S
#undef W16U
#undef W32S

    /* HMAC-SHA256 trailer */
    if (hmac_key && key_len > 0) {
        zl_hmac_sha256(hmac_key, key_len, buf, o, buf + o);
        o += ZL_HMAC_SIZE;
    }

    return (int)o;
}

/* ── Decoder ─────────────────────────────────────────────────────────────── */

zl_err_t zl_decode(
    const uint8_t *buf,
    size_t         buf_len,
    zl_frame_t    *out,
    zl_state_t    *mirror,
    const uint8_t *hmac_key,
    size_t         key_len)
{
    if (buf_len < ZL_HEADER_SIZE + ZL_PRESENCE_SIZE)
        return ZL_ERR_MALFORMED;

    /* HMAC verification */
    out->hmac_ok = true;
    size_t payload_end = buf_len;
    if (hmac_key && key_len > 0) {
        if (buf_len < ZL_HMAC_SIZE + 1)
            return ZL_ERR_MALFORMED;
        payload_end = buf_len - ZL_HMAC_SIZE;
        uint8_t expected[32];
        zl_hmac_sha256(hmac_key, key_len, buf, payload_end, expected);
        if (!_ct_eq(expected, buf + payload_end, 32)) {
            out->hmac_ok = false;
            return ZL_ERR_HMAC_FAIL;
        }
    }

    /* Parse header */
    size_t o = 0;
    uint16_t magic   = zl_r16(buf + o); o += 2;
    uint8_t  version = buf[o++];
    if (magic != ZL_MAGIC)      return ZL_ERR_BAD_MAGIC;
    if (version != ZL_VERSION && version != 1) return ZL_ERR_BAD_VERSION;

    out->seq          = zl_r32(buf + o); o += 4;
    out->timestamp_us = zl_r64(buf + o); o += 8;
    out->flags        = buf[o++];
    out->presence     = buf[o++];

    /* Unpack fields, updating mirror in-place */
#define R16S()   (o += 2, zl_r16s(buf + o - 2))
#define R16U()   (o += 2, zl_r16(buf + o - 2))
#define R32S()   (o += 4, zl_r32s(buf + o - 4))

    if (out->presence & ZL_PRESENCE_ATTITUDE) {
        if (o + 6 > payload_end) return ZL_ERR_MALFORMED;
        mirror->pitch = (float)R16S() / ZL_SCALE_ANGLE;
        mirror->yaw   = (float)R16S() / ZL_SCALE_ANGLE;
        mirror->roll  = (float)R16S() / ZL_SCALE_ANGLE;
    }
    if (out->presence & ZL_PRESENCE_ANGULAR_VEL) {
        if (o + 6 > payload_end) return ZL_ERR_MALFORMED;
        mirror->omega_x = (float)R16S() / ZL_SCALE_ANG_VEL;
        mirror->omega_y = (float)R16S() / ZL_SCALE_ANG_VEL;
        mirror->omega_z = (float)R16S() / ZL_SCALE_ANG_VEL;
    }
    if (out->presence & ZL_PRESENCE_POSITION) {
        if (o + 12 > payload_end) return ZL_ERR_MALFORMED;
        mirror->latitude  = (double)R32S() / ZL_SCALE_LAT_LON;
        mirror->longitude = (double)R32S() / ZL_SCALE_LAT_LON;
        mirror->altitude  = (double)R32S() / ZL_SCALE_ALT;
    }
    if (out->presence & ZL_PRESENCE_SOLAR_VOLTAGE) {
        if (o + 2 > payload_end) return ZL_ERR_MALFORMED;
        mirror->solar_voltage = (float)R16U() / ZL_SCALE_VOLTAGE;
    }
    if (out->presence & ZL_PRESENCE_BATTERY_VOLTAGE) {
        if (o + 2 > payload_end) return ZL_ERR_MALFORMED;
        mirror->battery_voltage = (float)R16U() / ZL_SCALE_VOLTAGE;
    }
    if (out->presence & ZL_PRESENCE_CHASSIS_TEMP) {
        if (o + 2 > payload_end) return ZL_ERR_MALFORMED;
        mirror->chassis_temp = (float)R16S() / ZL_SCALE_TEMP;
    }
    if (out->presence & ZL_PRESENCE_CPU_TEMP) {
        if (o + 2 > payload_end) return ZL_ERR_MALFORMED;
        mirror->cpu_temp = (float)R16S() / ZL_SCALE_TEMP;
    }
    if (out->presence & ZL_PRESENCE_RSSI) {
        if (o + 2 > payload_end) return ZL_ERR_MALFORMED;
        mirror->rssi = (float)R16S() / ZL_SCALE_RSSI;
    }

#undef R16S
#undef R16U
#undef R32S

    out->fields = *mirror;
    return ZL_OK;
}
