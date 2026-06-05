export namespace engine {
	
	export class CurvePoint {
	    timeSecs: number;
	    users: number;
	    curveIn: string;
	    exponent: number;
	
	    static createFrom(source: any = {}) {
	        return new CurvePoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeSecs = source["timeSecs"];
	        this.users = source["users"];
	        this.curveIn = source["curveIn"];
	        this.exponent = source["exponent"];
	    }
	}
	export class StepConfig {
	    url: string;
	    method: string;
	    headers: Record<string, string>;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new StepConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.method = source["method"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	    }
	}
	export class Config {
	    url: string;
	    method: string;
	    headers: Record<string, string>;
	    body: string;
	    steps?: StepConfig[];
	    mode: string;
	    concurrency: number;
	    rampUpSecs: number;
	    durationSecs: number;
	    curve: CurvePoint[];
	    noise: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.method = source["method"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.steps = this.convertValues(source["steps"], StepConfig);
	        this.mode = source["mode"];
	        this.concurrency = source["concurrency"];
	        this.rampUpSecs = source["rampUpSecs"];
	        this.durationSecs = source["durationSecs"];
	        this.curve = this.convertValues(source["curve"], CurvePoint);
	        this.noise = source["noise"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Metrics {
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
	
	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.elapsedSecs = source["elapsedSecs"];
	        this.totalRequests = source["totalRequests"];
	        this.successful = source["successful"];
	        this.clientErrors = source["clientErrors"];
	        this.rateLimited = source["rateLimited"];
	        this.serverErrors = source["serverErrors"];
	        this.networkErrors = source["networkErrors"];
	        this.errors = source["errors"];
	        this.rps = source["rps"];
	        this.errorRate = source["errorRate"];
	        this.p50Ms = source["p50Ms"];
	        this.p95Ms = source["p95Ms"];
	        this.p99Ms = source["p99Ms"];
	        this.currentConcurrency = source["currentConcurrency"];
	        this.running = source["running"];
	    }
	}

}

export namespace main {
	
	export class FlowRunStep {
	    name: string;
	    method: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowRunStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	    }
	}
	export class HeaderKV {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new HeaderKV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class RunConfig {
	    mode: string;
	    concurrency: number;
	    durationSecs: number;
	    curve?: engine.CurvePoint[];
	    noise?: number;
	    flowId?: string;
	    flowName?: string;
	    steps?: FlowRunStep[];
	
	    static createFrom(source: any = {}) {
	        return new RunConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.concurrency = source["concurrency"];
	        this.durationSecs = source["durationSecs"];
	        this.curve = this.convertValues(source["curve"], engine.CurvePoint);
	        this.noise = source["noise"];
	        this.flowId = source["flowId"];
	        this.flowName = source["flowName"];
	        this.steps = this.convertValues(source["steps"], FlowRunStep);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Sample {
	    t: number;
	    tickRps: number;
	    tickRpsOk: number;
	    p50: number;
	    p95: number;
	    p99: number;
	    conc: number;
	
	    static createFrom(source: any = {}) {
	        return new Sample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.tickRps = source["tickRps"];
	        this.tickRpsOk = source["tickRpsOk"];
	        this.p50 = source["p50"];
	        this.p95 = source["p95"];
	        this.p99 = source["p99"];
	        this.conc = source["conc"];
	    }
	}
	export class SampleResponse {
	    status: number;
	    statusText: string;
	    headers: Record<string, string>;
	    body: string;
	    bodyBytes: number;
	    bodyTruncated: boolean;
	    contentType: string;
	    elapsedMs: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SampleResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.bodyBytes = source["bodyBytes"];
	        this.bodyTruncated = source["bodyTruncated"];
	        this.contentType = source["contentType"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	    }
	}
	export class SavedFlow {
	    id: string;
	    name: string;
	    stepIds: string[];
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedFlow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.stepIds = source["stepIds"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class SavedRequest {
	    id: string;
	    name: string;
	    method: string;
	    url: string;
	    headers: HeaderKV[];
	    body: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], HeaderKV);
	        this.body = source["body"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SavedRun {
	    id: string;
	    startedAt: number;
	    name: string;
	    method: string;
	    url: string;
	    config: RunConfig;
	    metrics: engine.Metrics;
	    history: Sample[];
	
	    static createFrom(source: any = {}) {
	        return new SavedRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.config = this.convertValues(source["config"], RunConfig);
	        this.metrics = this.convertValues(source["metrics"], engine.Metrics);
	        this.history = this.convertValues(source["history"], Sample);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

