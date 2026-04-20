import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointGroupSalvoTallyCommandParams } from './params';
/**
 * CrossPointGroupSalvoTally command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointGroupSalvoTallyCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointGroupSalvoTallyCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointGroupSalvoTallyCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointGroupSalvoTallyCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isDestinationIdOutOfRange()) {
            errors.destinationId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceIdOutOfRange()) {
            errors.sourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isSalvoIdOutOfRange()) {
            errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isConnectIndexIdOutOfRange()) {
            errors.connectIndexId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * returns a boolean indicating if matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * returns a boolean indicating if levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }
    /**
     * returns a boolean indicating if destinationId is out of range
     * + false = destinationId is included within the limits
     * + true = destinationId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDestinationIdOutOfRange(): boolean {
        return this.data.destinationId < 0 || this.data.destinationId > 65535;
    }

    /**
     * returns a boolean indicating if sourceId is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSourceIdOutOfRange(): boolean {
        return this.data.sourceId < 0 || this.data.sourceId > 65535;
    }

    /**
     * returns a boolean indicating if salvoId is out of range
     * + false = salvoId is included within the limits
     * + true = salvoId [0-127] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSalvoIdOutOfRange(): boolean {
        return this.data.salvoId < 0 || this.data.salvoId > 127;
    }

    /**
     * returns a boolean indicating if ConnectIndex is out of range
     * + false = ConnectIndex is included within the limits
     * + true = ConnectIndex [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CrossPointSalvoGroupInterrogateMessageCommandParamsValidator
     */
    private isConnectIndexIdOutOfRange(): boolean {
        return this.data.connectIndex < 0 || this.data.connectIndex > 65535;
    }
}
