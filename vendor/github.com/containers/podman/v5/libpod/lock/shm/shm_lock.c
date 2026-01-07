#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "shm_lock.h"

// Compute the size of the SHM struct
static size_t compute_shm_size(uint32_t num_bitmaps) {
  return sizeof(shm_struct_t) + (num_bitmaps * sizeof(lock_group_t));
}

// Take the given mutex.
// Handles exceptional conditions, including a mutex locked by a process that
// died holding it.
// Returns 0 on success, or positive errno on failure.
static int take_mutex(pthread_mutex_t *mutex, bool trylock) {
  int ret_code;

  if (!trylock) {
    do {
      ret_code = pthread_mutex_lock(mutex);
    } while(ret_code == EAGAIN);
  } else {
    do {
      ret_code = pthread_mutex_trylock(mutex);
    } while(ret_code == EAGAIN);
  }

  if (ret_code == EOWNERDEAD) {
    // The previous owner of the mutex died while holding it
    // Take it for ourselves
    ret_code = pthread_mutex_consistent(mutex);
    if (ret_code != 0) {
      // Someone else may have gotten here first and marked the state consistent
      // However, the mutex could also be invalid.
      // Fail here instead of looping back to trying to lock the mutex.
      return ret_code;
    }
  } else if (ret_code != 0) {
    return ret_code;
  }

  return 0;
}

// Release the given mutex.
// Returns 0 on success, or positive errno on failure.
static int release_mutex(pthread_mutex_t *mutex) {
  int ret_code;

  do {
    ret_code = pthread_mutex_unlock(mutex);
  } while(ret_code == EAGAIN);

  if (ret_code != 0) {
    return ret_code;
  }

  return 0;
}

// Set up an SHM segment holding locks for libpod.
// num_locks must not be 0.
// Path is the path to the SHM segment. It must begin with a single / and
// container no other / characters, and be at most 255 characters including
// terminating NULL byte.
// Returns a valid pointer on success or NULL on error.
// If an error occurs, negative ERRNO values will be written to error_code.
shm_struct_t *setup_lock_shm(char *path, uint32_t num_locks, int *error_code) {
  int shm_fd, i, j, ret_code;
  uint32_t num_bitmaps;
  size_t shm_size;
  shm_struct_t *shm;
  pthread_mutexattr_t attr;

  // If error_code doesn't point to anything, we can't reasonably return errors
  // So fail immediately
  if (error_code == NULL) {
    return NULL;
  }

  // We need a nonzero number of locks
  if (num_locks == 0) {
    *error_code = -1 * EINVAL;
    return NULL;
  }

  if (path == NULL) {
    *error_code = -1 * EINVAL;
    return NULL;
  }

  // Calculate the number of bitmaps required
  num_bitmaps = num_locks / BITMAP_SIZE;
  if (num_locks % BITMAP_SIZE != 0) {
    // The actual number given is not an even multiple of our bitmap size
    // So round up
    num_bitmaps += 1;
  }

  // Calculate size of the shm segment
  shm_size = compute_shm_size(num_bitmaps);

  // Create a new SHM segment for us
  shm_fd = shm_open(path, O_RDWR | O_CREAT | O_EXCL, 0600);
  if (shm_fd < 0) {
    *error_code = -1 * errno;
    return NULL;
  }

  // Increase its size to what we need
  ret_code = ftruncate(shm_fd, shm_size);
  if (ret_code < 0) {
    *error_code = -1 * errno;
    goto CLEANUP_UNLINK;
  }

  // Map the shared memory in
  shm = mmap(NULL, shm_size, PROT_READ | PROT_WRITE, MAP_SHARED, shm_fd, 0);
  if (shm == MAP_FAILED) {
    *error_code = -1 * errno;
    goto CLEANUP_UNLINK;
  }

  // We have successfully mapped the memory, now initialize the region
  shm->magic = MAGIC;
  shm->unused = 0;
  shm->num_locks = num_bitmaps * BITMAP_SIZE;
  shm->num_bitmaps = num_bitmaps;

  // Create an initializer for our pthread mutexes
  ret_code = pthread_mutexattr_init(&attr);
  if (ret_code != 0) {
    *error_code = -1 * ret_code;
    goto CLEANUP_UNMAP;
  }

  // Ensure that recursive locking of a mutex by the same OS thread (which may
  // refer to numerous goroutines) blocks.
  ret_code = pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_NORMAL);
  if (ret_code != 0) {
    *error_code = -1 * ret_code;
    goto CLEANUP_FREEATTR;
  }

  // Set mutexes to pshared - multiprocess-safe
  ret_code = pthread_mutexattr_setpshared(&attr, PTHREAD_PROCESS_SHARED);
  if (ret_code != 0) {
    *error_code = -1 * ret_code;
    goto CLEANUP_FREEATTR;
  }

  // Set mutexes to robust - if a process dies while holding a mutex, we'll get
  // a special error code on the next attempt to lock it.
  // This should prevent panicking processes from leaving the state unusable.
  ret_code = pthread_mutexattr_setrobust(&attr, PTHREAD_MUTEX_ROBUST);
  if (ret_code != 0) {
    *error_code = -1 * ret_code;
    goto CLEANUP_FREEATTR;
  }

  // Initialize the mutex that protects the bitmaps using the mutex attributes
  ret_code = pthread_mutex_init(&(shm->segment_lock), &attr);
  if (ret_code != 0) {
    *error_code = -1 * ret_code;
    goto CLEANUP_FREEATTR;
  }

  // Initialize all bitmaps to 0 initially
  // And initialize all semaphores they use
  for (i = 0; i < num_bitmaps; i++) {
    shm->locks[i].bitmap = 0;
    for (j = 0; j < BITMAP_SIZE; j++) {
      // Initialize each mutex
      ret_code = pthread_mutex_init(&(shm->locks[i].locks[j]), &attr);
      if (ret_code != 0) {
	*error_code = -1 * ret_code;
	goto CLEANUP_FREEATTR;
      }
    }
  }

  // Close the file descriptor, we're done with it
  // Ignore errors, it's ok if we leak a single FD and this should only run once
  close(shm_fd);

  // Destroy the pthread initializer attribute.
  // Again, ignore errors, this will only run once and we might leak a tiny bit
  // of memory at worst.
  pthread_mutexattr_destroy(&attr);

  return shm;

  // Cleanup after an error
 CLEANUP_FREEATTR:
  pthread_mutexattr_destroy(&attr);
 CLEANUP_UNMAP:
  munmap(shm, shm_size);
 CLEANUP_UNLINK:
  close(shm_fd);
  shm_unlink(path);
  return NULL;
}

// Open an existing SHM segment holding libpod locks.
// num_locks is the number of locks that will be configured in the SHM segment.
// num_locks cannot be 0.
// Path is the path to the SHM segment. It must begin with a single / and
// container no other / characters, and be at most 255 characters including
// terminating NULL byte.
// Returns a valid pointer on success or NULL on error.
// If an error occurs, negative ERRNO values will be written to error_code.
// ERANGE is returned for a mismatch between num_locks and the number of locks
// available in the SHM lock struct.
shm_struct_t *open_lock_shm(char *path, uint32_t num_locks, int *error_code) {
  int shm_fd;
  shm_struct_t *shm;
  size_t shm_size;
  uint32_t num_bitmaps;

  if (error_code == NULL) {
    return NULL;
  }

  // We need a nonzero number of locks
  if (num_locks == 0) {
    *error_code = -1 * EINVAL;
    return NULL;
  }

  if (path == NULL) {
    *error_code = -1 * EINVAL;
    return NULL;
  }

  // Calculate the number of bitmaps required
  num_bitmaps = num_locks / BITMAP_SIZE;
  if (num_locks % BITMAP_SIZE != 0) {
    num_bitmaps += 1;
  }

  // Calculate size of the shm segment
  shm_size = compute_shm_size(num_bitmaps);

  shm_fd = shm_open(path, O_RDWR, 0600);
  if (shm_fd < 0) {
    *error_code = -1 * errno;
    return NULL;
  }

  // Map the shared memory in
  shm = mmap(NULL, shm_size, PROT_READ | PROT_WRITE, MAP_SHARED, shm_fd, 0);
  if (shm == MAP_FAILED) {
    *error_code = -1 * errno;
  }

  // Ignore errors, it's ok if we leak a single FD since this only runs once
  close(shm_fd);

  // Check if we successfully mmap'd
  if (shm == MAP_FAILED) {
    return NULL;
  }

  // Need to check the SHM to see if it's actually our locks
  if (shm->magic != MAGIC) {
    *error_code = -1 * EBADF;
    goto CLEANUP;
  }
  if (shm->num_locks != (num_bitmaps * BITMAP_SIZE)) {
    *error_code = -1 * ERANGE;
    goto CLEANUP;
  }

  return shm;

 CLEANUP:
  munmap(shm, shm_size);
  return NULL;
}

// Close an open SHM lock struct, unmapping the backing memory.
// The given shm_struct_t will be rendered unusable as a result.
// On success, 0 is returned. On failure, negative ERRNO values are returned.
int32_t close_lock_shm(shm_struct_t *shm) {
  int ret_code;
  size_t shm_size;

  // We can't unmap null...
  if (shm == NULL) {
    return -1 * EINVAL;
  }

  shm_size = compute_shm_size(shm->num_bitmaps);

  ret_code = munmap(shm, shm_size);

  if (ret_code != 0) {
    return -1 * errno;
  }

  return 0;
}

// Allocate the first available semaphore
// Returns a positive integer guaranteed to be less than UINT32_MAX on success,
// or negative errno values on failure
// On success, the returned integer is the number of the semaphore allocated
int64_t allocate_semaphore(shm_struct_t *shm) {
  int ret_code, i;
  bitmap_t test_map;
  int64_t sem_number, num_within_bitmap;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  // Lock the semaphore controlling access to our shared memory
  ret_code = take_mutex(&(shm->segment_lock), false);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Loop through our bitmaps to search for one that is not full
  for (i = 0; i < shm->num_bitmaps; i++) {
    if (shm->locks[i].bitmap != 0xFFFFFFFF) {
      test_map = 0x1;
      num_within_bitmap = 0;
      while (test_map != 0) {
	if ((test_map & shm->locks[i].bitmap) == 0) {
	  // Compute the number of the semaphore we are allocating
	  sem_number = (BITMAP_SIZE * i) + num_within_bitmap;
	  // OR in the bitmap
	  shm->locks[i].bitmap = shm->locks[i].bitmap | test_map;

	  // Clear the mutex
	  ret_code = release_mutex(&(shm->segment_lock));
	  if (ret_code != 0) {
	    return -1 * ret_code;
	  }

	  // Return the semaphore we've allocated
	  return sem_number;
	}
	test_map = test_map << 1;
	num_within_bitmap++;
      }
      // We should never fall through this loop
      // TODO maybe an assert() here to panic if we do?
    }
  }

  // Clear the mutex
  ret_code = release_mutex(&(shm->segment_lock));
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // All bitmaps are full
  // We have no available semaphores, report allocation failure
  return -1 * ENOSPC;
}

// Allocate the semaphore with the given ID.
// Returns an error if the semaphore with this ID does not exist, or has already
// been allocated.
// Returns 0 on success, or negative errno values on failure.
int32_t allocate_given_semaphore(shm_struct_t *shm, uint32_t sem_index) {
  int bitmap_index, index_in_bitmap, ret_code;
  bitmap_t test_map;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  // Check if the lock index is valid
  if (sem_index >= shm->num_locks) {
    return -1 * EINVAL;
  }

  bitmap_index = sem_index / BITMAP_SIZE;
  index_in_bitmap = sem_index % BITMAP_SIZE;

  // This should never happen if the sem_index test above succeeded, but better
  // safe than sorry
  if (bitmap_index >= shm->num_bitmaps) {
    return -1 * EFAULT;
  }

  test_map = 0x1 << index_in_bitmap;

  // Lock the mutex controlling access to our shared memory
  ret_code = take_mutex(&(shm->segment_lock), false);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Check if the semaphore is allocated
  if ((test_map & shm->locks[bitmap_index].bitmap) != 0) {
    ret_code = release_mutex(&(shm->segment_lock));
    if (ret_code != 0) {
      return -1 * ret_code;
    }

    return -1 * EEXIST;
  }

  // The semaphore is not allocated, allocate it
  shm->locks[bitmap_index].bitmap = shm->locks[bitmap_index].bitmap | test_map;

  ret_code = release_mutex(&(shm->segment_lock));
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  return 0;
}

// Deallocate a given semaphore
// Returns 0 on success, negative ERRNO values on failure
int32_t deallocate_semaphore(shm_struct_t *shm, uint32_t sem_index) {
  bitmap_t test_map;
  int bitmap_index, index_in_bitmap, ret_code;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  // Check if the lock index is valid
  if (sem_index >= shm->num_locks) {
    return -1 * EINVAL;
  }

  bitmap_index = sem_index / BITMAP_SIZE;
  index_in_bitmap = sem_index % BITMAP_SIZE;

  // This should never happen if the sem_index test above succeeded, but better
  // safe than sorry
  if (bitmap_index >= shm->num_bitmaps) {
    return -1 * EFAULT;
  }

  test_map = 0x1 << index_in_bitmap;

  // Lock the mutex controlling access to our shared memory
  ret_code = take_mutex(&(shm->segment_lock), false);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Check if the semaphore is allocated
  if ((test_map & shm->locks[bitmap_index].bitmap) == 0) {
    ret_code = release_mutex(&(shm->segment_lock));
    if (ret_code != 0) {
      return -1 * ret_code;
    }

    return -1 * ENOENT;
  }

  // The semaphore is allocated, clear it
  // Invert the bitmask we used to test to clear the bit
  test_map = ~test_map;
  shm->locks[bitmap_index].bitmap = shm->locks[bitmap_index].bitmap & test_map;

  ret_code = release_mutex(&(shm->segment_lock));
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  return 0;
}

// Deallocate all semaphores unconditionally.
// Returns negative ERRNO values.
int32_t deallocate_all_semaphores(shm_struct_t *shm) {
  int ret_code;
  uint i;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  // Lock the mutex controlling access to our shared memory
  ret_code = take_mutex(&(shm->segment_lock), false);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Iterate through all bitmaps and reset to unused
  for (i = 0; i < shm->num_bitmaps; i++) {
    shm->locks[i].bitmap = 0;
  }

  // Unlock the allocation control mutex
  ret_code = release_mutex(&(shm->segment_lock));
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  return 0;
}

// Lock a given semaphore
// Does not check if the semaphore is allocated - this ensures that, even for
// removed containers, we can still successfully lock to check status (and
// subsequently realize they have been removed).
// Returns 0 on success, -1 on failure
int32_t lock_semaphore(shm_struct_t *shm, uint32_t sem_index) {
  int bitmap_index, index_in_bitmap;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  if (sem_index >= shm->num_locks) {
    return -1 * EINVAL;
  }

  bitmap_index = sem_index / BITMAP_SIZE;
  index_in_bitmap = sem_index % BITMAP_SIZE;

  return -1 * take_mutex(&(shm->locks[bitmap_index].locks[index_in_bitmap]), false);
}

// Unlock a given semaphore
// Does not check if the semaphore is allocated - this ensures that, even for
// removed containers, we can still successfully lock to check status (and
// subsequently realize they have been removed).
// Returns 0 on success, -1 on failure
int32_t unlock_semaphore(shm_struct_t *shm, uint32_t sem_index) {
  int bitmap_index, index_in_bitmap;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  if (sem_index >= shm->num_locks) {
    return -1 * EINVAL;
  }

  bitmap_index = sem_index / BITMAP_SIZE;
  index_in_bitmap = sem_index % BITMAP_SIZE;

  return -1 * release_mutex(&(shm->locks[bitmap_index].locks[index_in_bitmap]));
}

// Get the number of free locks.
// Returns a positive integer guaranteed to be less than UINT32_MAX on success,
// or negative errno values on failure.
// On success, the returned integer is the number of free semaphores.
int64_t available_locks(shm_struct_t *shm) {
  int ret_code, i, count;
  bitmap_t test_map;
  int64_t free_locks = 0;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  // Lock the semaphore controlling access to the SHM segment.
  // This isn't strictly necessary as we're only reading, but it seems safer.
  ret_code = take_mutex(&(shm->segment_lock), false);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Loop through all bitmaps, counting number of allocated locks.
  for (i = 0; i < shm->num_bitmaps; i++) {
    // Short-circuit to catch fully-empty bitmaps quick.
    if (shm->locks[i].bitmap == 0) {
      free_locks += sizeof(bitmap_t) * 8;
      continue;
    }

    // Use Kernighan's Algorithm to count bits set. Subtract from number of bits
    // in the integer to get free bits, and thus free lock count.
    test_map = shm->locks[i].bitmap;
    count = 0;
    while (test_map) {
      test_map = test_map & (test_map - 1);
      count++;
    }

    free_locks += (sizeof(bitmap_t) * 8) - count;
  }

  // Clear the mutex
  ret_code = release_mutex(&(shm->segment_lock));
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  // Return free lock count.
  return free_locks;
}

// Attempt to take a given semaphore. If successfully taken, it is immediately
// released before the function returns.
// Used to check if a semaphore is in use, to detect potential deadlocks where a
// lock has not been released for an extended period of time.
// Note that this is NOT POSIX trylock as the lock is immediately released if
// taken.
// Returns negative errno on failure.
// On success, returns 1 if the lock was successfully taken, and 0 if it was
// not.
int32_t try_lock(shm_struct_t *shm, uint32_t sem_index) {
  int bitmap_index, index_in_bitmap, ret_code;
  pthread_mutex_t *mutex;

  if (shm == NULL) {
    return -1 * EINVAL;
  }

  if (sem_index >= shm->num_locks) {
    return -1 * EINVAL;
  }

  bitmap_index = sem_index / BITMAP_SIZE;
  index_in_bitmap = sem_index % BITMAP_SIZE;

  mutex = &(shm->locks[bitmap_index].locks[index_in_bitmap]);

  ret_code = take_mutex(mutex, true);

  if (ret_code == EBUSY) {
    // Did not successfully take the lock
    return 0;
  } else if (ret_code != 0) {
    // Another, unrelated error
    return -1 * ret_code;
  }

  // Lock taken successfully, unlock and return.
  ret_code = release_mutex(mutex);
  if (ret_code != 0) {
    return -1 * ret_code;
  }

  return 1;
}
