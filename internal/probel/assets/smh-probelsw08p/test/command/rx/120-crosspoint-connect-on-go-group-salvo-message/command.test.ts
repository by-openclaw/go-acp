import { CrossPointConnectOnGoSalvoGroupMessageCommand } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/command';
import { CrossPointConnectOnGoSalvoGroupMessageCommandParams } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { CrossPointConnectOnGoSalvoGroupMessageCommandItems } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/items';
import { BufferUtility } from '../../../../src/common/utility/buffer.utility';

describe('Crosspoint Connect On Go Group Salvo Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            for (let itemIndex = 880; itemIndex < 1024; itemIndex++) {
                // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                buildDataArray.push({
                    matrixId: 0,
                    levelId: 0,
                    destinationId: itemIndex,
                    sourceId: itemIndex,
                    salvoId: 0
                });
            }

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const metacommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);

            // Assert
            expect(metacommand).toBeDefined();
            expect(metacommand.params).toBe(params);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                salvoId: -1
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const fct = () => new CrossPointConnectOnGoSalvoGroupMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                // expect(localeDataError.validationErrors?.matrixId.id).toBe(
                //     CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.levelId.id).toBe(
                //     CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.destinationId.id).toBe(
                //     CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.sourceId.id).toBe(
                //     CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.salvoId.id).toBe(
                //     CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                expect(localeDataError.validationErrors?.salvoGroupMessageCommand.id).toBe(
                    CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                salvoId: 128
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const fct = () => new CrossPointConnectOnGoSalvoGroupMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                // expect(localeDataError.validationErrors?.matrixId.id).toBe(
                //     CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.levelId.id).toBe(
                //     CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.destinationId.id).toBe(
                //     CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.sourceId.id).toBe(
                //     CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                // expect(localeDataError.validationErrors?.salvoId.id).toBe(
                //     CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                // );
                expect(localeDataError.validationErrors?.salvoGroupMessageCommand.id).toBe(
                    CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Crosspoint Connect On Go Group Salvo Message CMD_120_0X78', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 0)', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 0; itemIndex < 2; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 0,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: '78 00 00 00 00 00',
                            bytesCount: 6,
                            checksum: 0x82,
                            buffer: '10 02 78 00 00 00 00 00 06 82 10 03'
                        },
                        {
                            data: '78 00 00 01 01 00',
                            bytesCount: 6,
                            checksum: 0x80,
                            buffer: '10 02 78 00 00 01 01 00 06 80 10 03'
                        }
                    ]
                );
            });
            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 895)', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 895; itemIndex < 896; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 0,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: '78 00 66 7f 7f 00',
                            bytesCount: 6,
                            checksum: 0x1e,
                            buffer: '10 02 78 00 66 7f 7f 00 06 1e 10 03'
                        }
                    ]
                );
            });
            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 1023)', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 895; itemIndex < 896; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 0,
                        destinationId: itemIndex,
                        sourceId: 1023,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: '78 00 67 7f 7f 00',
                            bytesCount: 6,
                            checksum: 0x1d,
                            buffer: '10 02 78 00 67 7f 7f 00 06 1d 10 03'
                        }
                    ]
                );
            });
        });

        describe('Extended General CrossPoint Connect Message CMD_248_0Xf8', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 0 - destinationId = 0 - sourceId = 0) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 0; itemIndex < 1; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 16,
                        levelId: 0,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: 'f8 10 00 00 00 00 00 00',
                            bytesCount: 8,
                            checksum: 0xf0,
                            buffer: '10 02 f8 10 10 00 00 00 00 00 00 08 f0 10 03'
                        }
                    ]
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 16 - destinationId = 0 - sourceId = 0) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 0; itemIndex < 1; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 16,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: 'f8 00 10 00 00 00 00 00',
                            bytesCount: 8,
                            checksum: 0xf0,
                            buffer: '10 02 f8 00 10 10 00 00 00 00 00 08 f0 10 03'
                        }
                    ]
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896 - sourceId = 0) destinationId > 895', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 896; itemIndex < 897; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 0,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data: 'f8 00 00 03 80 03 80 00',
                            bytesCount: 8,
                            checksum: 0xfa,
                            buffer: '10 02 f8 00 00 03 80 03 80 00 08 fa 10 03'
                        }
                    ]
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 1024) sourceId > 1023', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 1024; itemIndex < 1025; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: 0,
                        levelId: 0,
                        destinationId: 895,
                        sourceId: itemIndex,
                        salvoId: 0
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data:
                                'f8 00 00 03 7f 04 00 00',
                            bytesCount: 8,
                            checksum: 0x7a,
                            buffer:
                                '10 02 f8 00 00 03 7f 04 00 00 08 7a 10 03'
                        }
                    ]
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16 - sourceId = 16))', () => {
                // Arrange
                // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
                const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

                for (let itemIndex = 16; itemIndex < 17; itemIndex++) {
                    // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                    // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                    buildDataArray.push({
                        matrixId: itemIndex,
                        levelId: itemIndex,
                        destinationId: itemIndex,
                        sourceId: itemIndex,
                        salvoId: itemIndex
                    });
                }

                const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                    salvoGroupMessageCommandItems: buildDataArray
                };

                // Act
                const metaCommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(
                    metaCommand,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    [
                        {
                            data:
                                'f8 10 10 00 10 00 10 10',
                            bytesCount: 8,
                            checksum: 0xb0,
                            buffer:
                                '10 02 f8 10 10 10 10 00 10 10 00 10 10 10 10 08 b0 10 03'
                        }
                    ]
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            for (let itemIndex = 0; itemIndex < 2; itemIndex++) {
                // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                buildDataArray.push({
                    matrixId: 0,
                    levelId: 0,
                    destinationId: itemIndex,
                    sourceId: itemIndex,
                    salvoId: 0
                });
            }

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const metacommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
            const description = metacommand.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            for (let itemIndex = 896; itemIndex < 1024; itemIndex++) {
                // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                buildDataArray.push({
                    matrixId: 0,
                    levelId: 0,
                    destinationId: itemIndex,
                    sourceId: itemIndex,
                    salvoId: 0
                });
            }

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const metacommand = new CrossPointConnectOnGoSalvoGroupMessageCommand(params);
            const description = metacommand.toLogDescription();
        });
    });
});
