import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

import { http } from '@/lib/http';
import type { Agent, ApiResult } from '@/types';
import { baseFixtures } from '@/mocks/fixtures';

interface Credentials {
  email: string;
  password: string;
}

type AuthStatus = 'idle' | 'loading' | 'authenticated';

const bypassAuth = import.meta.env.DEV && import.meta.env.VITE_BYPASS_AUTH === 'true';
const defaultDevUser = bypassAuth ? baseFixtures.agents[0] : null;
const defaultDevToken = bypassAuth ? 'bypass-token' : null;

interface AuthState {
  token: string | null;
  user: Agent | null;
  status: AuthStatus;
  login: (credentials: Credentials) => Promise<ApiResult<{ token: string }>>;
  fetchMe: () => Promise<void>;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: defaultDevToken,
      user: defaultDevUser,
      status: bypassAuth ? 'authenticated' : 'idle',
      login: async (credentials) => {
        if (bypassAuth) {
          const token = defaultDevToken ?? 'bypass-token';
          set({ token, user: defaultDevUser, status: 'authenticated' });
          return { data: { token } };
        }
        set({ status: 'loading' });
        const result = await http.post<{ token: string }>('/auth/login', credentials);

        if (result.data) {
          set({ token: result.data.token });
          await get().fetchMe();
          return result;
        }

        set({ status: 'idle' });
        return result;
      },
      fetchMe: async () => {
        if (bypassAuth) {
          set({ user: defaultDevUser, status: 'authenticated' });
          return;
        }

        const { token } = get();
        if (!token) {
          return;
        }
        set({ status: 'loading' });
        const profile = await http.get<Agent>('/me');
        if (profile.data) {
          set({ user: profile.data, status: 'authenticated' });
        } else {
          set({ token: null, user: null, status: 'idle' });
        }
      },
      logout: () => {
        set({ token: null, user: null, status: 'idle' });
      },
    }),
    {
      name: 'arn-auth',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ token: state.token, user: state.user }),
      onRehydrateStorage: () => (state) => {
        if (state?.token) {
          state.fetchMe().catch(() => {
            state.logout();
          });
        }
      },
    },
  ),
);
