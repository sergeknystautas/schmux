import type { TerminalRecording } from './types';

const CSI = '\x1b[';
const CYAN = `${CSI}36m`;
const GREEN = `${CSI}32m`;
const DIM = `${CSI}2m`;
const BOLD = `${CSI}1m`;
const RESET = `${CSI}0m`;

export const newAgentRecording: TerminalRecording = {
  sessionId: 'demo-sess-new',
  frames: [
    {
      delay: 0,
      data: `${DIM}$ agent --prompt "Build a responsive dashboard component"${RESET}\n`,
    },
    { delay: 800, data: `\n${CYAN}●${RESET} Scanning project structure...\n` },
    { delay: 1200, data: `${DIM}  Found React 18, Tailwind CSS, TypeScript${RESET}\n` },
    { delay: 900, data: `${DIM}  Reading existing component patterns...${RESET}\n` },
    { delay: 1500, data: `\n${CYAN}●${RESET} Creating dashboard component...\n` },
    {
      delay: 2000,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Dashboard.tsx${RESET}\n`,
    },
    { delay: 1200, data: `${GREEN}✓${RESET} Created ${BOLD}src/components/StatCard.tsx${RESET}\n` },
    { delay: 1000, data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Chart.tsx${RESET}\n` },
    { delay: 800, data: `\n${CYAN}●${RESET} Adding responsive breakpoints...\n` },
    { delay: 1500, data: `${GREEN}✓${RESET} Updated ${BOLD}src/styles/dashboard.css${RESET}\n` },
    { delay: 1000, data: `\n${CYAN}●${RESET} Writing tests...\n` },
    {
      delay: 2500,
      data: `${GREEN}✓${RESET} Created ${BOLD}src/components/Dashboard.test.tsx${RESET} ${DIM}(6 tests)${RESET}\n`,
    },
    { delay: 1500, data: `${GREEN}✓${RESET} 6/6 tests passed\n` },
    {
      delay: 500,
      data: `\n${GREEN}${BOLD}Done.${RESET} Dashboard component built with responsive layout.\n`,
    },
  ],
};
