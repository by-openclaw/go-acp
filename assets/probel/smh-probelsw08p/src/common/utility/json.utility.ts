import * as CJSON from 'circular-json';

import { Constants } from '../util/constants';

export abstract class JsonUtility {
    protected constructor() {
        throw new Error(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
    }

    static stringify(obj: any, replacer: any = null, space = 2): string {
        return CJSON.stringify(obj, replacer, space);
    }

    static stringifyMap(map: Map<any, any>): string {
        const selfIterator = (m: any): any =>
            Array.from(m).reduce((acc: any, [key, value]: any) => {
                if (value instanceof Map) {
                    acc[key] = selfIterator(value);
                } else {
                    acc[key] = value;
                }

                return acc;
            }, {});
        const res = selfIterator(map);
        return this.stringify(res);
    }

    static safeJSONParse(data: string): any {
        try {
            return JSON.parse(data);
        } catch (e) {
            return undefined;
        }
    }
}
