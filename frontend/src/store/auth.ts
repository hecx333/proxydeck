import { create } from "zustand";
import { persist } from "zustand/middleware";

interface AuthState {
  username: string | null;
  setUsername: (username: string | null) => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      username: null,
      setUsername: (username) => set({ username })
    }),
    {
      name: "proxydeck-admin-auth"
    }
  )
);
