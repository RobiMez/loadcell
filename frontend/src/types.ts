// Shared types lifted out of App.svelte so view components and stores can
// import them without depending on the host component. These mirror Go-side
// types but stay defined locally so the frontend can hold its own opinions
// (relaxed unions, optional fields) without leaning on wails-generated
// classes that are awkward to extend.

export type Metrics = {
  elapsedSecs: number;
  totalRequests: number;
  successful: number;
  clientErrors: number;
  rateLimited: number;
  serverErrors: number;
  networkErrors: number;
  errors: number;
  rps: number;
  errorRate: number;
  p50Ms: number;
  p95Ms: number;
  p99Ms: number;
  currentConcurrency: number;
  running: boolean;
};

export type Sample = {
  t: number;
  tickRps: number;
  tickRpsOk: number;
  p50: number;
  p95: number;
  p99: number;
  conc: number;
};

export type FlowRunStep = { name: string; method: string; url: string };

export type CurveType = 'linear' | 'exponential' | 'step';

export type CurvePt = {
  timeSecs: number;
  users: number;
  curveIn: CurveType;
  exponent: number;
};

export type RunConfig = {
  mode: 'constant' | 'curve' | 'imported';
  concurrency: number;
  durationSecs: number;
  curve?: CurvePt[];
  noise?: number;
  // Populated only when the run was a compound flow. Single-request runs
  // leave these undefined.
  flowId?: string;
  flowName?: string;
  steps?: FlowRunStep[];
};

export type Run = {
  id: string;
  startedAt: number; // ms epoch
  name: string;
  method: string;
  url: string;
  config: RunConfig;
  metrics: Metrics;
  history: Sample[];
};

export type HeaderRow = { key: string; value: string };

// Compound flow as held in the frontend (the wails-generated class is
// `main.SavedFlow`; this alias keeps view components SDK-agnostic).
export type SavedFlowT = {
  id: string;
  name: string;
  stepIds: string[];
  createdAt: string;
  updatedAt: string;
};

export const METHODS = [
  'GET',
  'POST',
  'PUT',
  'PATCH',
  'DELETE',
  'HEAD',
  'OPTIONS',
] as const;
