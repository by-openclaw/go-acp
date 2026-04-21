import { Constants } from '../util/constants';
import { Maybe } from '../util/type';

export abstract class TypeUtility {
    protected constructor() {
        throw new Error(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
    }

    static getValueOrUndefined(data: Maybe<any>): any {
        return data === null || data === undefined ? undefined : data;
    }
}
