import { useSyncExternalStore } from 'react';

const query = '(max-width: 768px)';

function subscribe(onStoreChange: () => void) {
  const mediaQuery = window.matchMedia(query);
  mediaQuery.addEventListener('change', onStoreChange);
  return () => mediaQuery.removeEventListener('change', onStoreChange);
}

function getSnapshot() {
  return window.matchMedia(query).matches;
}

function getServerSnapshot() {
  return false;
}

export function useMediaQuery() {
  return { isOpen: useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot) };
}
