import { create } from 'zustand';

interface UIState {
  rangeHours: number;
  setRangeHours: (h: number) => void;
}
export const useUI = create<UIState>((set) => ({
  rangeHours: 24,
  setRangeHours: (h) => set({ rangeHours: h }),
}));
