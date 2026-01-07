#ifndef GO_GPGME_H
#define GO_GPGME_H

#define _FILE_OFFSET_BITS 64
#include <stdint.h>

#include <gpgme.h>

extern ssize_t gogpgme_readfunc(void *handle, void *buffer, size_t size);
extern ssize_t gogpgme_writefunc(void *handle, void *buffer, size_t size);
extern off_t gogpgme_seekfunc(void *handle, off_t offset, int whence);
extern gpgme_error_t gogpgme_passfunc(void *hook, char *uid_hint, char *passphrase_info, int prev_was_bad, int fd);
extern gpgme_off_t gogpgme_data_seek(gpgme_data_t dh, gpgme_off_t offset, int whence);

extern gpgme_error_t gogpgme_op_assuan_transact_ext(gpgme_ctx_t ctx, char *cmd, void *data_h, void *inquiry_h , void *status_h, gpgme_error_t *operr);

extern gpgme_error_t gogpgme_assuan_data_callback(void *opaque, void* data, size_t datalen );
extern gpgme_error_t gogpgme_assuan_inquiry_callback(void *opaque, char* name, char* args);
extern gpgme_error_t gogpgme_assuan_status_callback(void *opaque, char* status, char* args);

extern unsigned int key_revoked(gpgme_key_t k);
extern unsigned int key_expired(gpgme_key_t k);
extern unsigned int key_disabled(gpgme_key_t k);
extern unsigned int key_invalid(gpgme_key_t k);
extern unsigned int key_can_encrypt(gpgme_key_t k);
extern unsigned int key_can_sign(gpgme_key_t k);
extern unsigned int key_can_certify(gpgme_key_t k);
extern unsigned int key_secret(gpgme_key_t k);
extern unsigned int key_can_authenticate(gpgme_key_t k);
extern unsigned int key_is_qualified(gpgme_key_t k);
extern unsigned int signature_wrong_key_usage(gpgme_signature_t s);
extern unsigned int signature_pka_trust(gpgme_signature_t s);
extern unsigned int signature_chain_model(gpgme_signature_t s);
extern unsigned int subkey_revoked(gpgme_subkey_t k);
extern unsigned int subkey_expired(gpgme_subkey_t k);
extern unsigned int subkey_disabled(gpgme_subkey_t k);
extern unsigned int subkey_invalid(gpgme_subkey_t k);
extern unsigned int subkey_secret(gpgme_subkey_t k);
extern unsigned int uid_revoked(gpgme_user_id_t u);
extern unsigned int uid_invalid(gpgme_user_id_t u);

#endif
