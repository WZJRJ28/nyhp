import { nanoid } from 'nanoid';
import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

export type ThemeMode = 'light' | 'dark';

export interface ToastMessage {
  id: string;
  title: string;
  description?: string;
  type?: 'info' | 'success' | 'error';
}

interface UIState {
  theme: ThemeMode;
  toasts: ToastMessage[];
  setTheme: (mode: ThemeMode) => void;
  toggleTheme: () => void;
  pushToast: (toast: Omit<ToastMessage, 'id'> & { id?: string }) => void;
  dismissToast: (id: string) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      theme: 'light',
      toasts: [],
      setTheme: (mode) => set({ theme: mode }),
      toggleTheme: () => {
        const next = get().theme === 'dark' ? 'light' : 'dark';
        set({ theme: next });
      },
      pushToast: (toast) => {
        const id = toast.id ?? nanoid();
        set((state) => ({ toasts: [...state.toasts, { ...toast, id }] }));
      },
      dismissToast: (id) => {
        set((state) => ({ toasts: state.toasts.filter((item) => item.id !== id) }));
      },
    }),
    {
      name: 'arn-ui',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ theme: state.theme }),
    },
  ),
);
