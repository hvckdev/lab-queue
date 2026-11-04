import { useEffect, useState, useSyncExternalStore } from 'react';
import type { LabQueueController } from '../types';
import { LabQueueStore } from './controller';

export type UseLabQueueResult = LabQueueController;

export function useLabQueue(): LabQueueController {
  const [store] = useState(() => new LabQueueStore());
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  useEffect(() => {
    store.start();
    return store.stop;
  }, [store]);
  return { ...state,
    setSubjectFilter: store.setSubjectFilter, setGroupFilter: store.setGroupFilter, selectSession: store.selectSession,
    clearNotice: store.clearNotice, endExpiredSession: store.endExpiredSession, retryBootstrap: store.retryBootstrap, refresh: store.refresh,
    login: store.login, register: store.register, logout: store.logout,
    createSession: store.createSession, joinQueue: store.joinQueue, callNext: store.callNext,
    closeRegistration: store.closeRegistration, finishSession: store.finishSession,
    cancelEntry: store.cancelEntry, completeEntry: store.completeEntry, skipEntry: store.skipEntry,
  };
}
