// entry point for the TS client test suite: import a module per tested unit
// (each registers its tests on import), then report
import { done } from './harness';
import './cluster_test';
import './damage_test';
import './layoutskip_test';
import './metrics_test';
import './wrap_test';

done();
