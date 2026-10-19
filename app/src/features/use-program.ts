/**
 * A programme being edited, saved as it changes.
 *
 * There is no Save button in the builder — edits land on the device a moment
 * after they are made, the way Notes works, and pending changes are flushed
 * when the screen goes away so the screen underneath reloads the latest.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useFocusEffect } from 'expo-router';

import { useApp } from '@/state/app';
import { getProgram, saveProgram, type ProgramDoc } from './programs';

const DELAY_MS = 500;

export function useProgram(id: string | undefined) {
  const { db, touch } = useApp();
  const [doc, setDoc] = useState<ProgramDoc | null>(null);
  const [loading, setLoading] = useState(true);
  const pending = useRef<ProgramDoc | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flush = useCallback(async () => {
    if (timer.current) { clearTimeout(timer.current); timer.current = null; }
    const next = pending.current;
    pending.current = null;
    if (next && db) {
      await saveProgram(db, next);
      touch();
    }
  }, [db, touch]);

  const load = useCallback(async () => {
    if (!db || !id) return;
    setLoading(true);
    setDoc(await getProgram(db, id));
    setLoading(false);
  }, [db, id]);

  // Reload on focus, so returning from the day editor shows its edits —
  // unless this screen has its own edits still waiting to save.
  useFocusEffect(useCallback(() => {
    if (!pending.current) void load();
    return () => { void flush(); };
  }, [load, flush]));

  useEffect(() => () => { void flush(); }, [flush]);

  const update = useCallback((change: (d: ProgramDoc) => ProgramDoc) => {
    setDoc((current) => {
      if (!current) return current;
      const next = change(current);
      pending.current = next;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => { void flush(); }, DELAY_MS);
      return next;
    });
  }, [flush]);

  return { doc, loading, update, flush, reload: load };
}
