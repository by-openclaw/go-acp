import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointSalvoGroupInterrogateMessageCommandParams } from './params';

/**
 * CrossPointSalvoGroupInterrogateMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointSalvoGroupInterrogateMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointSalvoGroupInterrogateMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointSalvoGroupInterrogateMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {(Record<string, LocaleData>)} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isConnectIndexIdOutOfRange()) {
            errors.connectIndexId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSalvoIdOutOfRange()) {
            errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the isConnectIndexId is out of range
     * + false = isConnectIndexId is included within the limits
     * + true = isConnectIndexId  [0-65535] is out of range
     *
     * @private
     * @returns {boolean} 'true' if ConnectIndexId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isConnectIndexIdOutOfRange(): boolean {
        return this.data.connectIndexId < 0 || this.data.connectIndexId > 65535;
    }

    /**
     * Gets a boolean indicating whether the salvoId is out of range
     * + false = salvoId is included within the limits
     * + true = salvoId [0-127] is out of range
     *
     * @private
     * @returns {boolean} 'true' if salvoId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isSalvoIdOutOfRange(): boolean {
        return this.data.salvoId < 0 || this.data.salvoId > 127;
    }
}
