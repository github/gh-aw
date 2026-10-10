export interface OutcomeResult {
  result: string;
  outcome_status: string;
  evidence_strength: string;
  signal: string;
  detail: string;
  resolution_sec: number | null;
  pending_age_sec: number | null;
  review_comments: number | null;
  changed_files: number | null;
  additions: number | null;
  deletions: number | null;
  reactions_total: number | null;
  reactions_positive: number | null;
  reactions_negative: number | null;
  comments: number | null;
  human_comments?: number;
  human_reviews?: number;
  human_edits?: number;
  zero_touch: boolean;
}
